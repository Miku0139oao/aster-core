import { execFile as execFileCb, spawn } from "node:child_process";
import { promisify } from "node:util";
import { stat } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const execFile = promisify(execFileCb);

const scriptDirectory = dirname(fileURLToPath(import.meta.url));
const docsDirectory = resolve(scriptDirectory, "..");
const clientDirectory = resolve(docsDirectory, "dist", "client");

const args = new Set(process.argv.slice(2));
const dryRun = args.has("--dry-run");
const noSymlink = args.has("--no-symlink");

function getArg(name) {
  for (const a of args) {
    if (a.startsWith(`${name}=`)) return a.slice(name.length + 1);
  }
  return undefined;
}

const host =
  getArg("--host") ??
  process.env.DOCS_DEPLOY_HOST ??
  "root@miku.zerotwo02.net";
const releaseBase =
  getArg("--release-base") ??
  process.env.DOCS_RELEASE_BASE ??
  "/srv/astercore-docs/releases";
const currentSymlink =
  getArg("--current") ??
  process.env.DOCS_CURRENT_SYMLINK ??
  "/srv/astercore-docs/current";

const date = new Intl.DateTimeFormat("en-CA", {
  timeZone: "Asia/Shanghai",
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
})
  .format(new Date())
  .replace(/-/g, "");
const sha = (
  await execFile("git", ["-C", docsDirectory, "rev-parse", "--short=8", "HEAD"])
).stdout.trim();
const releaseName = `${date}-${sha}`;
const releaseDirectory = `${releaseBase}/${releaseName}`;

const clientStat = await stat(clientDirectory);
if (!clientStat.isDirectory()) {
  throw new Error(
    `Missing build output: ${clientDirectory}. Run "npm run build" first.`,
  );
}

const remoteCommand = noSymlink
  ? `mkdir -p "${releaseDirectory}" && tar -xzf - -C "${releaseDirectory}"`
  : `set -e; mkdir -p "${releaseDirectory}" && tar -xzf - -C "${releaseDirectory}" && ln -sfn "${releaseDirectory}" "${currentSymlink}"`;

if (dryRun) {
  console.log("[DRY-RUN] client:", clientDirectory);
  console.log("[DRY-RUN] host:", host);
  console.log("[DRY-RUN] release:", releaseDirectory);
  console.log("[DRY-RUN] current:", noSymlink ? "(skipped)" : currentSymlink);
  console.log("[DRY-RUN] remote:", remoteCommand);
  process.exit(0);
}

const tar = spawn("tar", ["-czf", "-", "-C", clientDirectory, "."], {
  stdio: ["ignore", "pipe", "pipe"],
});
const ssh = spawn("ssh", [host, remoteCommand], {
  stdio: ["pipe", "pipe", "pipe"],
});

tar.stdout.pipe(ssh.stdin);
tar.stderr.on("data", (data) => process.stderr.write(`tar: ${data}`));
ssh.stderr.on("data", (data) => process.stderr.write(`ssh: ${data}`));

ssh.on("close", (code) => {
  if (code === 0) {
    console.log(`Deployed to ${releaseDirectory}`);
    if (!noSymlink) console.log(`Symlinked ${currentSymlink} -> ${releaseName}`);
  } else {
    console.error(`ssh exited with code ${code}`);
    process.exit(code ?? 1);
  }
});

tar.on("error", (error) => {
  console.error("tar failed:", error);
  process.exit(1);
});

ssh.on("error", (error) => {
  console.error("ssh failed:", error);
  process.exit(1);
});
