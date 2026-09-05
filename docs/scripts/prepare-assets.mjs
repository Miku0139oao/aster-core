import { copyFile, cp, mkdir } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const scriptDirectory = dirname(fileURLToPath(import.meta.url));
const docsDirectory = resolve(scriptDirectory, "..");
const publicDirectory = resolve(
  docsDirectory,
  ".vitepress",
  "public-runtime",
);

await mkdir(publicDirectory, { recursive: true });

// VitePress uses public-runtime instead of its default public directory.
// Keep committed downloads in public and copy them into the generated tree.
await cp(resolve(docsDirectory, "public"), publicDirectory, {
  recursive: true,
});

for (const file of ["logo.png", "config.yaml"]) {
  await copyFile(
    resolve(docsDirectory, file),
    resolve(publicDirectory, file),
  );
}
