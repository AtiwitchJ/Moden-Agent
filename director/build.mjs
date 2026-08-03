import * as esbuild from "esbuild";
import { writeFile } from "node:fs/promises";

const result = await esbuild.build({
  entryPoints: ["src/index.ts"],
  bundle: true,
  platform: "node",
  target: "node18",
  format: "esm",
  banner: { js: "import { createRequire } from 'module'; const require = createRequire(import.meta.url);" },
  write: false,
});

// Keep the committed embedded bundle free of whitespace-only diff noise from
// third-party source comments. This does not alter executable code.
await writeFile("dist/index.js", result.outputFiles[0].text.replace(/[\t ]+$/gm, ""));
