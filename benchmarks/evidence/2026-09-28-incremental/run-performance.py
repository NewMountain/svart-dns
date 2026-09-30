import json
import os
import resource
import subprocess
import sys
from pathlib import Path

binary, output = sys.argv[1:]
scratch = Path(os.environ.get("TMPDIR", str(Path(output).resolve().parent / "tmp")))
scratch.mkdir(parents=True, exist_ok=True)
env = dict(os.environ, SVART_CORE_LARGE_BENCH="1", GOMAXPROCS="4", TMPDIR=str(scratch))
with (
    Path(output).open("x") as log,
    Path(output + ".resource.json").open("x") as resource_log,
):
    run = subprocess.run(
        [binary, "-test.run", "^TestListPerformanceThreeMillion$", "-test.v"],
        env=env,
        stdout=log,
        stderr=subprocess.STDOUT,
        check=False,
    )
    usage = resource.getrusage(resource.RUSAGE_CHILDREN)
    resource_log.write(
        json.dumps(
            {
                "exit_code": run.returncode,
                "peak_rss_kib": usage.ru_maxrss,
                "user_cpu_seconds": usage.ru_utime,
                "system_cpu_seconds": usage.ru_stime,
            },
            indent=2,
        )
        + "\n"
    )
raise SystemExit(run.returncode)
