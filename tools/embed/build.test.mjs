import path from "node:path";
/** Packaging regressions use synthetic output and never invoke Vite or providers. */
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, mkdir, writeFile, readFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { digest, packageOutput, rewriteIndex, validateNames, sourceDigest, virtualOwner, requireProduction, dependencyRoot, collectFontLicenses, ROOT } from "./build.mjs";

/** Produce a cyclic split graph whose chunk bytes must survive relocation. */
function fixture() {
  return [
    { type: "asset", fileName: "index.html", source: '<!doctype html><script type="module" src="./assets/main.js"></script><link href="./assets/app.css?theme=dark#x" rel="stylesheet">' },
    { type: "chunk", fileName: "assets/main.js", code: 'import "./lazy.js";', imports: ["assets/lazy.js"], dynamicImports: [] },
    { type: "chunk", fileName: "assets/lazy.js", code: 'import "./main.js";', imports: ["assets/main.js"], dynamicImports: [] },
    { type: "asset", fileName: "assets/app.css", source: "body{color:black}" },
  ];
}

test("preserves cyclic chunk bytes and index URL suffixes in a deterministic namespace", () => {
  const first = packageOutput(fixture());
  const second = packageOutput(fixture().reverse());
  assert.deepEqual(first, second);
  const metadata = JSON.parse(first.get("manifest.json"));
  const prefix = `assets/${metadata.bundle_sha256}/`;
  assert.equal(first.get(prefix + "assets/main.js").toString(), 'import "./lazy.js";');
  assert.match(first.get("index.html").toString(), /app\.css\?theme=dark#x/);
  for (const [name, entry] of Object.entries(metadata.files)) assert.equal(digest(first.get(name)), entry.sha256);
});

test("refuses missing chunks, undeclared index assets and ambiguous output names", () => {
  assert.throws(() => packageOutput(fixture().filter((item) => item.fileName !== "assets/lazy.js")), /Unpackaged/);
  assert.throws(() => rewriteIndex('<img src="missing.png">', new Set(), "assets/hash/"), /Undeclared/);
  assert.throws(() => rewriteIndex('<script>alert(1)</script>', new Set(), "assets/hash/"), /Inline/);
  for (const names of [["../a"], ["assets/a\nb"], ["assets/A", "assets/a"], ["assets//a"], ["assets/a%20b"]]) assert.throws(() => validateNames(names), /Unsafe/);
  assert.throws(() => packageOutput([...fixture(), { type: "asset", fileName: "worker.js", source: "bad" }]), /Unexpected/);
});

test("changes the namespace when any lazy asset changes", () => {
  const original = JSON.parse(packageOutput(fixture()).get("manifest.json"));
  const changed = fixture();
  changed[2].code += "export const changed=true;";
  const revised = JSON.parse(packageOutput(changed).get("manifest.json"));
  assert.notEqual(original.bundle_sha256, revised.bundle_sha256);
});

test("hashes binary inputs verbatim and normalizes only declared text", () => {
  const png = Buffer.from([137,80,78,71,13,10,26,10,255]);
  assert.equal(sourceDigest("image.png",png),digest(png));
  assert.equal(sourceDigest("file.tsx",Buffer.from("a\r\nb\r")),digest("a\nb\n"));
});

test("attributes Vite preload runtime and refuses nonproduction or unknown virtual runtime", () => {
  assert.equal(virtualOwner("\0vite/preload-helper.js"), "vite/package.json");
  assert.equal(virtualOwner("\0rolldown/runtime.js"), "rolldown/package.json");
  assert.throws(() => virtualOwner("\0unknown/runtime"), /Unattributed/);
  assert.throws(() => requireProduction("development"), /NODE_ENV/);
  requireProduction("production");
  requireProduction(undefined);
});

/** The early script remains external, blocking and inside the integrity namespace. */
test("packages the prepaint theme bootstrap and separate upstream and service key destinations", () => {
  const output=fixture();
  output[0].source='<html><head><script src="./assets/theme-init.js"></script><link href="./assets/app.css" rel="stylesheet"><script type="module" src="./assets/main.js"></script></head></html>';
  output.push({type:"asset",fileName:"assets/theme-init.js",source:'document.documentElement.dataset.theme="dark";'});
  const result=packageOutput(output);
  const index=result.get("index.html").toString();
  assert.ok(index.indexOf("theme-init.js") < index.indexOf("app.css"));
  assert.match(index, /<script src="[^"]+theme-init.js"><\/script>/);
  const manifest=JSON.parse(result.get("manifest.json"));
  assert.deepEqual(manifest.routes,["/app/","/app/login","/app/overview","/app/keys","/app/access-keys","/app/requests","/app/requests/:id","/app/settings"]);
});

/** Nested module-mode metadata does not own shipped code or its license. */
test("attributes nested ESM metadata and nested installations to their locked package roots", () => {
  const modules = path.join(ROOT, "web/node_modules");
  assert.equal(dependencyRoot(path.join(modules, "date-fns/esm/format/index.js")), path.join(modules, "date-fns"));
  assert.equal(dependencyRoot(path.join(modules, "@douyinfe/semi-ui/lib/es/button/index.js")), path.join(modules, "@douyinfe/semi-ui"));
  assert.equal(dependencyRoot(path.join(modules, "parent/node_modules/child/lib/index.js")), path.join(modules, "parent/node_modules/child"));
  assert.throws(() => dependencyRoot(path.resolve(ROOT, "../outside/node_modules/child/index.js")), /outside/);
});

test("ships vendored font license texts with a shard-set identity", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "exta-font-"));
  const font = { name: "SyntheticFont", version: "1.0", license: "OFL-1.1", root: path.join(root, "fonts") };
  await mkdir(path.join(font.root, "Regular"), { recursive: true });
  await mkdir(path.join(font.root, "LICENSES"), { recursive: true });
  await writeFile(path.join(font.root, "Regular", "a.woff2"), Buffer.from([0, 1, 2]));
  await writeFile(path.join(font.root, "Regular", "result.css"), "@font-face{}\r\n");
  await writeFile(path.join(font.root, "LICENSES", "OFL.txt"), "license text");
  const destination = path.join(root, "licenses");
  const entry = await collectFontLicenses(destination, font);
  assert.equal(entry.name, "SyntheticFont");
  assert.match(entry.integrity, /^sha256-[a-f0-9]{64}$/);
  assert.deepEqual(Object.keys(entry.files), ["SyntheticFont-1.0/OFL.txt"]);
  assert.equal((await readFile(path.join(destination, "SyntheticFont-1.0/OFL.txt"))).toString(), "license text");
  await writeFile(path.join(font.root, "Regular", "result.css"), "@font-face{}\n");
  assert.equal((await collectFontLicenses(destination, font)).integrity, entry.integrity);
  await writeFile(path.join(font.root, "Regular", "a.woff2"), Buffer.from([9]));
  assert.notEqual((await collectFontLicenses(destination, font)).integrity, entry.integrity);
});
