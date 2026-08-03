import * as esbuild from "esbuild";
import { copyFile, mkdir, writeFile } from "node:fs/promises";

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

for (const skillName of ["adhd", "git-workflow-and-versioning"]) {
  const bundledSkill = `../backend/internal/directorassets/bundle/skills/${skillName}`;
  await mkdir(bundledSkill, { recursive: true });
  await copyFile(`skills/${skillName}/SKILL.md`, `${bundledSkill}/SKILL.md`);
}
