import {
  cp,
  mkdir,
  rm,
} from "node:fs/promises";
import {
  basename,
  dirname,
  resolve,
} from "node:path";
import { fileURLToPath } from "node:url";

const scriptDirectory = dirname(fileURLToPath(import.meta.url));
const docsDirectory = resolve(scriptDirectory, "..");
const outputDirectory = resolve(docsDirectory, "dist");

if (
  dirname(outputDirectory) !== docsDirectory ||
  basename(outputDirectory) !== "dist"
) {
  throw new Error(`Refusing to replace unexpected output: ${outputDirectory}`);
}

await rm(outputDirectory, { recursive: true, force: true });
await mkdir(resolve(outputDirectory, "client"), { recursive: true });
await mkdir(resolve(outputDirectory, "server"), { recursive: true });

await cp(
  resolve(docsDirectory, ".vitepress", "dist"),
  resolve(outputDirectory, "client"),
  { recursive: true },
);
await cp(
  resolve(docsDirectory, "worker", "index.js"),
  resolve(outputDirectory, "server", "index.js"),
);
