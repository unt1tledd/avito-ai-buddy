import { existsSync, mkdirSync, mkdtempSync, readFileSync, statSync, writeFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { basename, dirname, extname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { compileFunction } from 'node:vm';
import ts from 'typescript';

const sourceRoot=fileURLToPath(new URL('../src/',import.meta.url));
const coverageRoot=process.env.NODE_V8_COVERAGE?fileURLToPath(new URL('../coverage/',import.meta.url)):null;

// Each load has its own module graph, keeping test mocks and module state isolated.
export function load(file,overrides={}) {
  const cache=new Map();
  const generatedDir=coverageRoot?(()=>{
    mkdirSync(coverageRoot,{recursive:true});
    return mkdtempSync(join(coverageRoot,'compiled-'));
  })():null;
  function evaluate(sourcePath) {
    if(cache.has(sourcePath))return cache.get(sourcePath).exports;
    const module={exports:{}};
    cache.set(sourcePath,module);
    const require=createRequire(sourcePath);
    const resolveImport=id=>{
      if(Object.hasOwn(overrides,id))return overrides[id];
      if(id.startsWith('.')) {
        const target=resolve(dirname(sourcePath),id);
        const stem=/\.[cm]?jsx?$/.test(target)?target.slice(0,-extname(target).length):target;
        const candidates=[target,`${stem}.ts`,`${stem}.tsx`,`${stem}.js`,`${stem}.jsx`,join(target,'index.ts'),join(target,'index.tsx')];
        const found=candidates.find(path=>existsSync(path) && statSync(path).isFile());
        if(found && /\.[jt]sx?$/.test(found))return evaluate(found);
      }
      return require(id);
    };
    const source=readFileSync(sourcePath,'utf8');
    const {outputText,sourceMapText}=ts.transpileModule(source,{
      fileName:sourcePath,
      compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2020,jsx:ts.JsxEmit.React,esModuleInterop:true,sourceMap:Boolean(generatedDir),inlineSources:Boolean(generatedDir)},
    });
    let code=outputText,filename=sourcePath;
    if(generatedDir) {
      filename=join(generatedDir,`${cache.size}-${basename(sourcePath)}.cjs`);
      const sourceMap=JSON.parse(sourceMapText);
      sourceMap.sources=[sourcePath];
      const encoded=Buffer.from(JSON.stringify(sourceMap)).toString('base64');
      code=outputText.replace(/sourceMappingURL=.*$/,`sourceMappingURL=data:application/json;base64,${encoded}`);
      writeFileSync(filename,code);
    }
    try {
      compileFunction(code,['require','module','exports','__filename','__dirname'],{filename})(resolveImport,module,module.exports,sourcePath,dirname(sourcePath));
    } catch(error) {
      cache.delete(sourcePath);
      throw error;
    }
    return module.exports;
  }
  return evaluate(resolve(sourceRoot,file));
}
