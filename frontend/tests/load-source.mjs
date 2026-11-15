import { mkdirSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { basename, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { compileFunction } from 'node:vm';
import ts from 'typescript';

const require = createRequire(import.meta.url);
const coverageDir = process.env.NODE_V8_COVERAGE
  ? fileURLToPath(new URL('../coverage/', import.meta.url))
  : null;

export function load(file, overrides = {}) {
  const sourcePath = fileURLToPath(new URL(`../src/${file}`, import.meta.url));
  const source = readFileSync(sourcePath, 'utf8');
  const { outputText, sourceMapText } = ts.transpileModule(source, {
    fileName: sourcePath,
    compilerOptions: {
      module: ts.ModuleKind.CommonJS,
      target: ts.ScriptTarget.ES2020,
      jsx: ts.JsxEmit.React,
      esModuleInterop: true,
      sourceMap: Boolean(coverageDir),
      inlineSources: Boolean(coverageDir),
    },
  });

  const module = { exports: {} };
  const resolve = id => overrides[id] || require(id);
  const args = ['require', 'module', 'exports'];

  if (coverageDir) {
    mkdirSync(coverageDir, { recursive: true });
    const generatedDir = mkdtempSync(join(coverageDir, 'compiled-'));
    const filename = join(generatedDir, `${basename(file)}.cjs`);
    const sourceMap = JSON.parse(sourceMapText);
    sourceMap.sources = [sourcePath];
    const encoded = Buffer.from(JSON.stringify(sourceMap)).toString('base64');
    const code = outputText.replace(
      /sourceMappingURL=.*$/,
      `sourceMappingURL=data:application/json;base64,${encoded}`,
    );

    writeFileSync(filename, code);
    compileFunction(code, args, { filename })(resolve, module, module.exports);
  } else {
    compileFunction(outputText, args, { filename: sourcePath })(
      resolve, module, module.exports,
    );
  }

  return module.exports;
}
