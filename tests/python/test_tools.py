"""Exercise the actual release helpers with disposable Git/files/processes."""

import contextlib
import gzip
import io
import json
import os
import runpy
import subprocess
import sys
import tempfile
import tomllib
import unittest
from pathlib import Path
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
SCANNER = ROOT / "scripts/check-public-archives.py"
RUNNER = ROOT / "benchmarks/evidence/2026-09-28-incremental/run-performance.py"


class ToolTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.work = Path(self.temporary.name)

    def invoke(self, script: Path, *args: str) -> tuple[int | str | None, str, str]:
        stdout, stderr = io.StringIO(), io.StringIO()
        with (
            contextlib.chdir(self.work),
            contextlib.redirect_stdout(stdout),
            contextlib.redirect_stderr(stderr),
            patch.object(sys, "argv", [str(script), *args]),
            self.assertRaises(SystemExit) as exit_result,
        ):
            runpy.run_path(str(script), run_name="__main__")
        return exit_result.exception.code, stdout.getvalue(), stderr.getvalue()

    def git(self, *args: str) -> None:
        subprocess.run(
            ["git", "-C", str(self.work), *args], check=True, capture_output=True
        )

    def test_every_owned_python_file_has_a_coverage_denominator(self) -> None:
        config = tomllib.loads((ROOT / "pyproject.toml").read_text())
        roots = config["tool"]["coverage"]["run"]["source"]
        tracked = (
            subprocess.run(
                ["git", "ls-files", "-z", "--", "*.py"],
                cwd=ROOT,
                capture_output=True,
                check=True,
            )
            .stdout.decode()
            .split("\0")
        )
        missing = sorted(
            name
            for name in tracked
            if name
            and not name.startswith("tests/python/")
            # The disposable collector assertion driver is a test fixture.
            and name != "tests/observability/verify.py"
            and not any(Path(name).is_relative_to(root) for root in roots)
        )
        self.assertEqual(missing, [])

    def archive(self, name: str, payload: bytes) -> None:
        (self.work / name).write_bytes(gzip.compress(payload, mtime=0))

    def test_archive_clean_untracked_and_non_gzip_scope(self) -> None:
        self.git("init", "-q")
        self.archive("clean.txt.gz", b"public.example\n")
        (self.work / "notes.txt").write_text("operator.internal\n")
        self.git("add", "clean.txt.gz", "notes.txt")
        self.archive("untracked.gz", b"operator.internal\n")
        self.assertEqual(self.invoke(SCANNER, r"operator\.internal", "."), (0, "", ""))

    def test_archive_scans_complete_lines_and_concatenated_members(self) -> None:
        self.git("init", "-q")
        self.archive(
            "full.gz",
            b"public.example\n"
            + b"x" * (2 * 1024 * 1024)
            + b" OPERATOR.INTERNAL operator.internal\n",
        )
        with (self.work / "full.gz").open("ab") as stream:
            stream.write(gzip.compress(b"\xff operator.internal\n", mtime=0))
        self.git("add", "full.gz")
        self.assertEqual(
            self.invoke(SCANNER, r"operator\.internal", "."),
            (
                1,
                "full.gz:2: private markers: OPERATOR.INTERNAL, "
                "operator.internal\nfull.gz:3: private markers: "
                "operator.internal\n",
                "",
            ),
        )

    def test_archive_corrupt_input_is_unavailable(self) -> None:
        self.git("init", "-q")
        (self.work / "broken.gz").write_bytes(b"not gzip\n")
        self.git("add", "broken.gz")
        self.assertEqual(
            self.invoke(SCANNER, "marker", "."),
            (
                2,
                "",
                "check-public: archive scan unavailable: Not a gzipped file (b'no')\n",
            ),
        )

    def test_archive_missing_tracked_file_is_unavailable(self) -> None:
        self.git("init", "-q")
        self.archive("removed.gz", b"public.example\n")
        self.git("add", "removed.gz")
        (self.work / "removed.gz").unlink()
        self.assertEqual(
            self.invoke(SCANNER, "marker", "."),
            (
                2,
                "",
                "check-public: archive scan unavailable: "
                "[Errno 2] No such file or directory: 'removed.gz'\n",
            ),
        )

    def test_archive_git_failure_is_unavailable(self) -> None:
        self.assertEqual(
            self.invoke(SCANNER, "marker", "."),
            (
                2,
                "",
                "check-public: archive scan unavailable: "
                "Command '['git', 'ls-files', '-z', '--', '.']' "
                "returned non-zero exit status 128.\n",
            ),
        )

    def benchmark_fixture(self, status: int) -> Path:
        binary = self.work / "benchmark-fixture"
        binary.write_text(
            f"#!{sys.executable}\n"
            "import json, os, sys\n"
            "print(json.dumps({'args': sys.argv[1:], "
            "'large': os.environ['SVART_CORE_LARGE_BENCH'], "
            "'cpu': os.environ['GOMAXPROCS'], "
            "'scratch': os.environ['TMPDIR']}), flush=True)\n"
            "print('complete evidence ' * 10000, flush=True)\n"
            "print('child stderr', file=sys.stderr)\n"
            f"sys.exit({status})\n"
        )
        binary.chmod(0o700)
        return binary

    def test_runner_complete_output_environment_and_failed_exit(self) -> None:
        binary = self.benchmark_fixture(7)
        output = self.work / "failed.log"
        scratch = self.work / "scratch"
        with patch.dict(os.environ, {"TMPDIR": str(scratch)}):
            self.assertEqual(self.invoke(RUNNER, str(binary), str(output)), (7, "", ""))
        lines = output.read_text().splitlines()
        # stderr may precede buffered stdout in a redirected child.
        self.assertEqual(
            sorted(lines),
            sorted(
                [
                    json.dumps(
                        {
                            "args": [
                                "-test.run",
                                "^TestListPerformanceThreeMillion$",
                                "-test.v",
                            ],
                            "large": "1",
                            "cpu": "4",
                            "scratch": str(scratch),
                        }
                    ),
                    "complete evidence " * 10000,
                    "child stderr",
                ]
            ),
        )
        metadata = json.loads(Path(str(output) + ".resource.json").read_text())
        self.assertEqual(
            sorted(metadata),
            ["exit_code", "peak_rss_kib", "system_cpu_seconds", "user_cpu_seconds"],
        )
        self.assertEqual(metadata["exit_code"], 7)
        self.assertGreater(metadata["peak_rss_kib"], 0)
        self.assertGreaterEqual(metadata["user_cpu_seconds"], 0)
        self.assertGreaterEqual(metadata["system_cpu_seconds"], 0)
        self.assertEqual(scratch.is_dir(), True)

    def test_runner_success_fallback_and_no_overwrite(self) -> None:
        binary = self.benchmark_fixture(0)
        output = self.work / "success.log"
        with patch.dict(os.environ):
            os.environ.pop("TMPDIR", None)
            self.assertEqual(self.invoke(RUNNER, str(binary), str(output)), (0, "", ""))
            original = output.read_bytes()
            metadata = Path(str(output) + ".resource.json")
            original_metadata = metadata.read_bytes()
            with self.assertRaises(FileExistsError):
                self.invoke(RUNNER, str(binary), str(output))
        self.assertEqual(
            (output.read_bytes(), metadata.read_bytes()), (original, original_metadata)
        )
        self.assertEqual((self.work / "tmp").is_dir(), True)
        self.assertIn(json.dumps(str(self.work / "tmp")), original.decode())

    def test_runner_preserves_existing_metadata_and_missing_executable(self) -> None:
        output = self.work / "unused.log"
        metadata = Path(str(output) + ".resource.json")
        metadata.write_bytes(b"original measurement\n")
        with self.assertRaises(FileExistsError):
            self.invoke(RUNNER, str(self.work / "missing"), str(output))
        self.assertEqual(metadata.read_bytes(), b"original measurement\n")
        with self.assertRaises(FileNotFoundError):
            self.invoke(RUNNER, str(self.work / "missing"), str(self.work / "new.log"))
        self.assertEqual(metadata.read_bytes(), b"original measurement\n")


if __name__ == "__main__":
    unittest.main()
