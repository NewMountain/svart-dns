import assert from "node:assert/strict";
import { promisify } from "node:util";
const execute = promisify(execFile);
import {
  spawn,
  execFile,
  execFileSync,
  type ChildProcess,
} from "node:child_process";
import { createSocket } from "node:dgram";
import {
  createServer as httpServer,
  type IncomingMessage,
  type ServerResponse,
} from "node:http";
import { createServer as httpsServer, get as httpsGet } from "node:https";
import { createServer as tcpServer, type Socket } from "node:net";
import { createServer as tlsServer } from "node:tls";
import { createWriteStream, readFileSync } from "node:fs";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";
import type { AddressInfo } from "node:net";

export const output = process.env["SVART_E2E_OUTPUT"] ?? "/evidence";
export const binary =
  process.env["SVART_E2E_BINARY"] ?? "/usr/local/bin/svart-dns";
export const base = "https://127.0.0.1:50300";
export const password = "e2e-fixture-only-password-2026";
export const username = "e2e-admin";
export const rawSnapshots = new Map<string, Map<number, string>>();
export const fixtureLists = new Map<string, string>([
  [
    "/list.txt",
    "blocked.example\n||wildcard.example^\n@@||safe.wildcard.example^\n",
  ],
]);
const apps = new Map<number, App>();
export const acceptedQueries: {
  domain: string;
  port: number;
  expected: string;
}[] = [];
export async function until(
  check: () => boolean | Promise<boolean>,
  description: string,
  timeout = 15000,
): Promise<void> {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    if (await check()) return;
    await new Promise<void>((resolve) => setTimeout(resolve, 50));
  }
  throw new Error(`Timed out: ${description}`);
}
export function sql(database: string, statement: string): unknown {
  const text = execFileSync("sqlite3", ["-json", database, statement], {
    encoding: "utf8",
  });
  return text.trim() === "" ? [] : JSON.parse(text);
}
export async function dns(
  domain: string,
  expected: "NOERROR" | "NXDOMAIN",
  label: string,
  port = 50153,
  expectedAddress = "203.0.113.42",
): Promise<void> {
  for (const mode of ["+notcp", "+tcp"]) {
    const { stdout: result } = await execute(
      "dig",
      [
        "@127.0.0.1",
        "-p",
        String(port),
        domain,
        "A",
        "+time=2",
        "+tries=1",
        mode,
      ],
      { encoding: "utf8" },
    );
    await writeFile(join(output, `${label}-${mode}.dns.txt`), result);
    assert.ok(result.includes(`status: ${expected}`), result);
    acceptedQueries.push({ domain: domain + ".", port, expected });
    const app = apps.get(port);
    assert.ok(app);
    await until(() => {
      snapshotRaw(app);
      return (
        (rawSnapshots.get(app.name)?.size ?? 0) >=
        acceptedQueries.filter((item) => item.port === port).length
      );
    }, "accepted DNS event durable journal ownership");
    await writeFile(
      join(output, "accepted-dns.json"),
      JSON.stringify(acceptedQueries, null, 2),
    );
    if (expected === "NOERROR")
      assert.ok(result.includes(expectedAddress), result);
  }
}

export class App {
  process: ChildProcess | undefined;
  readonly directory: string;
  readonly database: string;
  constructor(
    readonly name: string,
    readonly adminPort: number,
    readonly dnsPort: number,
    public externalIP = "192.0.2.10",
  ) {
    this.directory = join(output, name);
    this.database = join(this.directory, "main.sqlite");
    apps.set(dnsPort, this);
    rawSnapshots.set(name, new Map());
  }
  async start(): Promise<void> {
    await mkdir(this.directory, { recursive: true });
    const log = createWriteStream(
      join(this.directory, `server-${String(Date.now())}.log`),
      { mode: 0o600 },
    );
    const environment: NodeJS.ProcessEnv = {
      ...process.env,
      DB_PATH: this.database,
      ARCHIVE_PATH: join(this.directory, "archives"),
      DNS_PORT: String(this.dnsPort),
      ADMIN_PORT: String(this.adminPort),
      EXTERNAL_IP: this.externalIP,
      NODE_ID: this.name,
      LOG_LEVEL: "info",
      LOG_FORMAT: "json",
      ALLOW_PRIVATE_LIST_URLS: "true",
      TLS_CERT: join(output, "fixture.crt"),
      TLS_KEY: join(output, "fixture.key"),
      SYNC_SECRET: "isolated-fixture-secret-32-bytes-only",
      SYNC_INTERVAL: "1s",
      SYNC_TLS_SERVER_NAME: "localhost",
      SYNC_PEER_ALLOWLIST: "https://127.0.0.1:50300,https://127.0.0.1:50301",
      SSL_CERT_FILE: join(output, "fixture.crt"),
      GOMAXPROCS: "4",
    };
    delete environment["ADMIN_USER"];
    delete environment["ADMIN_PASSWORD"];
    this.process = spawn(binary, [], {
      env: environment,
      stdio: ["ignore", "pipe", "pipe"],
    });
    this.process.stdout?.pipe(log, { end: false });
    this.process.stderr?.pipe(log, { end: false });
    this.process.once("exit", () => log.end());
    await until(async () => {
      assert.equal(
        this.process?.exitCode,
        null,
        `${this.name} stopped before ready`,
      );
      return new Promise<boolean>((resolve) => {
        const request = httpsGet(
          `https://127.0.0.1:${String(this.adminPort)}/health`,
          { ca: readFileSync(join(output, "fixture.crt")) },
          (response) => {
            response.resume();
            resolve(response.statusCode === 200);
          },
        );
        request.on("error", () => {
          resolve(false);
        });
      });
    }, `${this.name} health`);
  }
  async stop(): Promise<void> {
    const child = this.process;
    if (!child || child.exitCode !== null) return;
    const result = new Promise<void>((resolve, reject) =>
      child.once("exit", (code) => {
        if (code === 0) resolve();
        else reject(new Error(`${this.name} exited ${String(code)}`));
      }),
    );
    child.kill("SIGTERM");
    const deadline = setTimeout(() => {
      child.kill("SIGKILL");
    }, 20000);
    try {
      await result;
    } finally {
      clearTimeout(deadline);
      this.process = undefined;
    }
  }
  async setupToken(): Promise<string> {
    const { readdir } = await import("node:fs/promises");
    let token = "";
    await until(async () => {
      for (const file of await readdir(this.directory)) {
        if (!file.endsWith(".log")) continue;
        const text = await readFile(join(this.directory, file), "utf8");
        const match = /[A-Z2-7]{4}(?:-[A-Z2-7]{4}){7}/.exec(text);
        if (match) token = match[0];
      }
      return token !== "";
    }, "first-run setup token");
    return token;
  }
}

// A deliberately small DNS wire fixture: real transports, deterministic A responses.
function answer(query: Buffer): Buffer {
  assert.ok(query.length >= 17, "DNS fixture received a short query");
  let end = 12;
  while (query[end] !== 0) {
    const length = query[end];
    assert.notEqual(length, undefined);
    assert.ok(length !== undefined && length <= 63);
    end += length + 1;
    assert.ok(end < query.length);
  }
  const questionEnd = end + 5;
  assert.ok(questionEnd <= query.length);
  const response = Buffer.alloc(questionEnd + 16);
  query.copy(response, 0, 0, questionEnd);
  response.writeUInt16BE(0x8180, 2);
  response.writeUInt16BE(1, 4);
  response.writeUInt16BE(1, 6);
  response.writeUInt32BE(0, 8);
  response.writeUInt16BE(0xc00c, questionEnd);
  response.writeUInt16BE(1, questionEnd + 2);
  response.writeUInt16BE(1, questionEnd + 4);
  response.writeUInt32BE(60, questionEnd + 6);
  response.writeUInt16BE(4, questionEnd + 10);
  Buffer.from([203, 0, 113, 42]).copy(response, questionEnd + 12);
  return response;
}
function serveStream(socket: Socket): void {
  let pending = Buffer.alloc(0);
  socket.on("data", (chunk: Buffer) => {
    pending = Buffer.concat([pending, chunk]);
    while (pending.length >= 2) {
      const length = pending.readUInt16BE(0);
      if (pending.length < length + 2) return;
      const message = answer(pending.subarray(2, length + 2));
      const size = Buffer.alloc(2);
      size.writeUInt16BE(message.length);
      socket.write(Buffer.concat([size, message]));
      pending = pending.subarray(length + 2);
    }
  });
  socket.on("error", () => socket.destroy());
}
export async function fixtures(): Promise<() => Promise<void>> {
  execFileSync(
    "openssl",
    [
      "req",
      "-x509",
      "-newkey",
      "rsa:2048",
      "-nodes",
      "-keyout",
      join(output, "fixture.key"),
      "-out",
      join(output, "fixture.crt"),
      "-days",
      "2",
      "-subj",
      "/CN=localhost",
      "-addext",
      "subjectAltName=DNS:localhost,IP:127.0.0.1",
    ],
    { stdio: "ignore" },
  );
  const key = readFileSync(join(output, "fixture.key"));
  const cert = readFileSync(join(output, "fixture.crt"));
  const counts = { udp: 0, tcp: 0, doh: 0, dot: 0, lists: 0 };
  const save = () => {
    void writeFile(join(output, "fixture-counts.json"), JSON.stringify(counts));
  };
  const udp = createSocket("udp4");
  udp.on("message", (message, remote) => {
    counts.udp++;
    save();
    udp.send(answer(message), remote.port, remote.address);
  });
  const tcp = tcpServer((socket) => {
    counts.tcp++;
    save();
    serveStream(socket);
  });
  const dot = tlsServer({ key, cert }, (socket) => {
    counts.dot++;
    save();
    serveStream(socket);
  });
  const serveHTTP = async (
    request: IncomingMessage,
    response: ServerResponse,
  ): Promise<void> => {
    if (request.url === "/health") {
      response.end("ready");
      return;
    }
    const listBody = fixtureLists.get(request.url ?? "");
    if (listBody !== undefined) {
      counts.lists++;
      save();
      response.end(listBody);
      return;
    }
    const url = new URL(request.url ?? "/", "https://localhost");
    if (url.pathname === "/dns-query") {
      const pieces: Buffer[] = [];
      for await (const piece of request) {
        assert.ok(Buffer.isBuffer(piece));
        pieces.push(piece);
      }
      const query =
        request.method === "GET"
          ? Buffer.from(url.searchParams.get("dns") ?? "", "base64url")
          : Buffer.concat(pieces);
      counts.doh++;
      save();
      response.setHeader("Content-Type", "application/dns-message");
      response.end(answer(query));
      return;
    }
    response.writeHead(404);
    response.end("unknown fixture route");
  };
  const list = httpServer((request, response) => {
    void serveHTTP(request, response);
  });
  const doh = httpsServer({ key, cert }, (request, response) => {
    void serveHTTP(request, response);
  });
  await Promise.all([
    new Promise<void>((resolve) => udp.bind(50053, "127.0.0.1", resolve)),
    ...[
      [tcp, 50053],
      [dot, 50853],
      [list, 50800],
      [doh, 50443],
    ].map(([server, port]) => {
      assert.ok(
        typeof port === "number" &&
          server !== undefined &&
          typeof server !== "number",
      );
      return new Promise<void>((resolve) =>
        server.listen(port, "127.0.0.1", resolve),
      );
    }),
  ]);
  const addresses: AddressInfo[] = [tcp, dot, list, doh].map((server) => {
    const address = server.address();
    assert.ok(address && typeof address !== "string");
    return address;
  });
  await writeFile(
    join(output, "fixture-readiness.json"),
    JSON.stringify(addresses, null, 2),
  );
  return async () => {
    udp.close();
    for (const server of [tcp, dot, list, doh]) server.close();
    await writeFile(
      join(output, "fixture-counts.json"),
      JSON.stringify(counts, null, 2),
    );
  };
}

export function snapshotRaw(app: App): void {
  const records = sql(
    app.database + ".spool.sqlite",
    "SELECT id,hex(payload) AS payload FROM journal_records ORDER BY id",
  );
  assert.ok(Array.isArray(records));
  const originals = rawSnapshots.get(app.name);
  assert.ok(originals);
  for (const item of records) {
    const record: unknown = item;
    assert.ok(
      record &&
        typeof record === "object" &&
        "id" in record &&
        typeof record.id === "number" &&
        "payload" in record &&
        typeof record.payload === "string",
    );
    const previous = originals.get(record.id);
    if (previous !== undefined)
      assert.equal(record.payload, previous, "immutable raw payload changed");
    originals.set(record.id, record.payload);
  }
}
