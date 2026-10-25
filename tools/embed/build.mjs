/** Build and atomically promote the verified production UI and its provenance. */
import { createHash, randomUUID } from "node:crypto";
import { createRequire } from "node:module";
import { readFile, writeFile, readdir, mkdir, rename, lstat, copyFile } from "node:fs/promises";
import { execFileSync } from "node:child_process";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

export const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const WEB = path.join(ROOT, "web");
const requireWeb = createRequire(path.join(WEB, "package.json"));
const { parse, serialize } = await import(pathToFileURL(requireWeb.resolve("parse5")).href);
const ROUTES = ["/app/", "/app/login", "/app/overview", "/app/keys", "/app/access-keys", "/app/requests", "/app/requests/:id", "/app/settings"];

/** Hash exact payload bytes. */
export function digest(bytes) { return createHash("sha256").update(bytes).digest("hex"); }

/** Normalize only declared text formats; binary sources retain their exact bytes. */
export function sourceDigest(name, bytes) {
  return digest(/\.(tsx?|m?js|json|css|html|svg|go)$/.test(name) || [".node-version", ".npmrc"].includes(path.basename(name)) ? new TextDecoder("utf-8", { fatal: true, ignoreBOM: true }).decode(bytes).replace(/\r\n?/g, "\n") : bytes);
}

/** Attribute injected virtual runtime code to its package instead of dropping its license. */
export function virtualOwner(id) {
  if (id.startsWith("\0vite/")) return "vite/package.json";
  if (id.startsWith("\0rolldown/")) return "rolldown/package.json";
  if (id.startsWith("\0")) throw new Error(`Unattributed virtual runtime: ${id.slice(1)}`);
  return null;
}

/** Refuse development substitutions even when Vite's mode says production. */
export function requireProduction(environment) {
  if (environment && environment !== "production") throw new Error("NODE_ENV must be production");
}

/** Reject ambiguous or platform-dependent output names before serialization or writing. */
export function validateNames(names) {
  const seen = new Set();
  for (const name of names) {
    if (!/^[A-Za-z0-9_./-]+$/.test(name) || name.startsWith("/") || name.split("/").some((part) => !part || part === "." || part === "..") || seen.has(name.toLowerCase())) {
      throw new Error(`Unsafe or colliding asset path: ${name}`);
    }
    seen.add(name.toLowerCase());
  }
}

/** Walk regular source files without following links. */
async function filesUnder(directory) {
  const result = [];
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const filename = path.join(directory, entry.name);
    if (entry.isSymbolicLink()) throw new Error(`Source link refused: ${filename}`);
    if (entry.isDirectory()) result.push(...await filesUnder(filename));
    else if (entry.isFile()) result.push(filename);
    else throw new Error(`Nonregular source refused: ${filename}`);
  }
  return result;
}

/** Fingerprint frontend/build inputs with normalized text line endings across both targets. */
export async function sourceFiles() {
  const inputs = [...await filesUnder(path.join(WEB, "src")), ...await filesUnder(path.join(ROOT, "tools/embed")), ...await filesUnder(path.join(ROOT, "tools/embedpack"))];
  for (const name of ["web/index.html", "web/package.json", "web/package-lock.json", "web/vite.config.ts", "web/tsconfig.json", "api/contract-manifest.json", ".node-version", ".npmrc"]) inputs.push(path.join(ROOT, name));
  const result = {};
  for (const filename of inputs.sort()) {
    result[path.relative(ROOT, filename).split(path.sep).join("/")] = sourceDigest(filename, await readFile(filename));
  }
  return result;
}

/**
 * Rewrite parsed resource attributes and optionally expose a declared notice asset.
 * @param {string} html - Built HTML without inline executable content.
 * @param {Set<string>} names - Declared output paths.
 * @param {string} prefix - Verified immutable asset namespace.
 * @param {string} [noticeName] - Font notice asset to link from the document head.
 * @returns {string} Rewritten document with same-origin resource references.
 */
export function rewriteIndex(html, names, prefix, noticeName) {
  const document = parse(html);
  if (noticeName) {
    const head = document.childNodes.find((node) => node.tagName === "html").childNodes.find((node) => node.tagName === "head");
    head.childNodes.push({ nodeName: "link", tagName: "link", namespaceURI: "http://www.w3.org/1999/xhtml", attrs: [{ name: "rel", value: "license" }, { name: "href", value: `./${noticeName}` }], childNodes: [], parentNode: head });
  }
  const rewrite = (value) => {
    const url = new URL(value, "https://exta.invalid/");
    if (url.origin !== "https://exta.invalid" || !names.has(url.pathname.slice(1))) throw new Error(`Undeclared index resource: ${value}`);
    return `/app/${prefix}${url.pathname.slice(1)}${url.search}${url.hash}`;
  };
  const visit = (node) => {
    if (node.tagName === "base" || node.tagName === "style" || node.tagName === "script" && !node.attrs?.some((attribute) => attribute.name === "src")) throw new Error("Inline executable content is forbidden");
    for (const attribute of node.attrs ?? []) {
      if (attribute.name.startsWith("on") || attribute.name === "style") throw new Error("Inline execution/style attribute is forbidden");
      if (["src", "href", "poster"].includes(attribute.name)) attribute.value = rewrite(attribute.value);
      if (attribute.name === "srcset") attribute.value = attribute.value.split(",").map((candidate) => {
        const [url, ...descriptor] = candidate.trim().split(/\s+/);
        return [rewrite(url), ...descriptor].join(" ");
      }).join(", ");
    }
    for (const child of node.childNodes ?? []) visit(child);
    if (node.content) visit(node.content);
  };
  visit(document);
  return serialize(document) + "\n";
}

/**
 * Preserve the complete asset graph and font notices in one verified namespace.
 * @param {Array<object>} output - Vite's production chunks and assets.
 * @param {string} [fontNotices] - Complete font notices delivered with the page.
 * @returns {Map<string, Buffer>} Manifest-covered bundle files.
 */
export function packageOutput(output, fontNotices) {
  const payload = new Map();
  let index;
  for (const item of output) {
    if (item.fileName === ".vite/manifest.json") continue;
    const bytes = Buffer.from(item.type === "chunk" ? item.code : item.source);
    if (item.fileName === "index.html") { index = bytes.toString("utf8"); continue; }
    if (!item.fileName.startsWith("assets/")) throw new Error(`Unexpected build output: ${item.fileName}`);
    if (payload.has(item.fileName)) throw new Error("Duplicate build output");
    payload.set(item.fileName, bytes);
  }
  if (!index || payload.size === 0) throw new Error("Empty production build");
  const noticeName = fontNotices === undefined ? undefined : "assets/font-licenses.txt";
  if (noticeName) {
    if (typeof fontNotices !== "string" || !fontNotices.trim() || payload.has(noticeName)) throw new Error("Missing or conflicting font notices");
    payload.set(noticeName, Buffer.from(fontNotices));
  }
  validateNames([...payload.keys()]);
  for (const item of output.filter((entry) => entry.type === "chunk")) {
    for (const dependency of [...item.imports, ...item.dynamicImports]) if (!payload.has(dependency)) throw new Error(`Unpackaged chunk dependency: ${dependency}`);
  }
  const names = [...payload.keys()].sort((left, right) => Buffer.compare(Buffer.from(left), Buffer.from(right)));
  const bundleHash = digest(names.map((name) => `${name}\0${digest(payload.get(name))}\n`).join(""));
  const prefix = `assets/${bundleHash}/`;
  const files = new Map(names.map((name) => [prefix + name, payload.get(name)]));
  files.set("index.html", Buffer.from(rewriteIndex(index, new Set(names), prefix, noticeName)));
  const metadata = { format: 1, mode: "production", bundle_sha256: bundleHash, files: {}, routes: ROUTES };
  for (const [name, bytes] of files) metadata.files[name] = { size: bytes.length, sha256: digest(bytes), immutable: name !== "index.html" };
  files.set("manifest.json", Buffer.from(JSON.stringify(metadata, null, 2) + "\n"));
  return files;
}

/** Locate the installed package boundary, ignoring nested module-mode package.json files. */
export function dependencyRoot(id) {
  const filename = path.resolve(id.split("?")[0]);
  const marker = `${path.sep}node_modules${path.sep}`;
  const boundary = filename.lastIndexOf(marker);
  if (boundary < 0) return null;
  if (!filename.startsWith(WEB + path.sep)) throw new Error("Bundled dependency outside web workspace");
  const tail = filename.slice(boundary + marker.length).split(path.sep);
  const count = tail[0].startsWith("@") ? 2 : 1;
  if (tail.length <= count || tail.slice(0, count).some((part) => !part)) throw new Error("Invalid bundled package path");
  return path.join(filename.slice(0, boundary + marker.length), ...tail.slice(0, count));
}

/** Collect the exact installed package identities and license texts used in emitted chunks. */
async function collectLicenses(moduleIds, lock, destination) {
  const roots = new Set();
  for (const id of moduleIds) { const owner = virtualOwner(id); if (owner) roots.add(path.dirname(requireWeb.resolve(owner))); }
  for (const id of moduleIds) {
    const directory = dependencyRoot(id);
    if (directory) roots.add(directory);
  }
  const inventory = [];
  for (const directory of [...roots].sort()) {
    const metadata = JSON.parse(await readFile(path.join(directory, "package.json"), "utf8"));
    const relative = path.relative(WEB, directory).split(path.sep).join("/");
    const locked = lock.packages[relative];
    if (!locked || locked.version !== metadata.version || !locked.integrity) throw new Error(`Unverified bundled dependency: ${metadata.name}`);
    const entry = { name: metadata.name, version: metadata.version, integrity: locked.integrity, license: metadata.license, files: {} };
    const names = (await readdir(directory)).filter((name) => /^(license|licence|copying|notice)(\.|$)/i.test(name));
    if (!names.length) throw new Error(`Missing bundled dependency license: ${metadata.name}`);
    for (const name of names) {
      const bytes = await readFile(path.join(directory, name));
      const target = `${metadata.name.replaceAll("/", "_")}-${metadata.version}/${name}`;
      await mkdir(path.dirname(path.join(destination, target)), { recursive: true });
      await writeFile(path.join(destination, target), bytes);
      entry.files[target] = digest(bytes);
    }
    inventory.push(entry);
  }
  if (!inventory.length) throw new Error("No shipped dependency inventory");
  return inventory;
}

/** Vendored web font shipped in the UI, with its retrieved component license texts. */
export const FONT = { name: "JetBrainsLxgwNerdMono", version: "1.3", license: "OFL-1.1", root: path.join(WEB, "src/assets/fonts") };

/**
 * Reject unreviewed font, CSS or notice bytes before publishing a production bundle.
 * @param {object} font - Font identity and vendored directory.
 * @returns {Promise<void>} Resolves only for the exact recorded source set.
 */
export async function verifyFontProvenance(font = FONT) {
  const provenance = JSON.parse(await readFile(path.join(font.root, "provenance.json"), "utf8"));
  if (provenance.format !== 1 || provenance.font.name !== font.name || provenance.font.version !== font.version || provenance.font.license !== font.license) throw new Error("Font provenance identity mismatch");
  const expected = { ...provenance.files };
  for (const [name, hash] of Object.entries(provenance.licenses)) expected[`LICENSES/${name}`] = hash;
  validateNames(Object.keys(expected));
  const actual = (await filesUnder(font.root)).filter((name) => name !== path.join(font.root, "provenance.json"));
  if (actual.length !== Object.keys(expected).length) throw new Error("Font provenance source set changed");
  for (const name of actual) {
    const relative = path.relative(font.root, name).split(path.sep).join("/");
    if (expected[relative] !== digest(await readFile(name))) throw new Error(`Font provenance bytes changed: ${relative}`);
  }
}

/**
 * Collect full font notices and verifiable source records for browser recipients.
 * @param {object} font - Font identity and vendored directory.
 * @returns {Promise<string>} Complete human-readable notice and provenance text.
 */
export async function collectFontNotices(font = FONT) {
  const directory = path.join(font.root, "LICENSES");
  const sections = [`${font.name} ${font.version}\nFont data: ${font.license}. Companion tooling notices are preserved separately.`];
  for (const name of (await readdir(directory)).sort()) sections.push(`===== ${name} =====\n${await readFile(path.join(directory, name), "utf8")}`);
  sections.push(`===== Source provenance =====\n${await readFile(path.join(font.root, "provenance.json"), "utf8")}`);
  return sections.join("\n\n") + "\n";
}

/** Record the vendored font shards' identity and copy their license texts into the bundle evidence. */
export async function collectFontLicenses(destination, font = FONT) {
  await verifyFontProvenance(font);
  const licenseRoot = path.join(font.root, "LICENSES");
  const shards = (await filesUnder(font.root)).filter((name) => !name.startsWith(licenseRoot + path.sep)).sort();
  if (!shards.some((name) => name.endsWith(".woff2"))) throw new Error("Missing vendored font shards");
  const records = [];
  for (const name of shards) records.push(`${path.relative(font.root, name).split(path.sep).join("/")}\0${sourceDigest(name, await readFile(name))}\n`);
  const entry = { name: font.name, version: font.version, integrity: `sha256-${digest(records.join(""))}`, license: font.license, files: {} };
  const licenses = (await readdir(licenseRoot)).sort();
  if (!licenses.length) throw new Error(`Missing font license texts: ${font.name}`);
  for (const name of licenses) {
    const bytes = await readFile(path.join(licenseRoot, name));
    const target = `${font.name}-${font.version}/${name}`;
    await mkdir(path.dirname(path.join(destination, target)), { recursive: true });
    await writeFile(path.join(destination, target), bytes);
    entry.files[target] = digest(bytes);
  }
  const provenance = await readFile(path.join(font.root, "provenance.json"));
  const target = `${font.name}-${font.version}/provenance.json`;
  await writeFile(path.join(destination, target), provenance);
  entry.files[target] = digest(provenance);
  return entry;
}

/** Build once, validate all outputs, then promote one directory with rollback on rename failure. */
export async function build() {
  const before = await sourceFiles();
  const packageMetadata = JSON.parse(await readFile(path.join(WEB, "package.json"), "utf8"));
  if (process.versions.node !== packageMetadata.engines.node) throw new Error("Use the pinned Node toolchain");
  const npmVersion = process.env.npm_config_user_agent?.match(/npm\/([^ ]+)/)?.[1];
  if (npmVersion !== packageMetadata.engines.npm) throw new Error("Run npm run build with the pinned npm toolchain");
  const contract = JSON.parse(await readFile(path.join(ROOT, "api/contract-manifest.json"), "utf8"));
  if (contract.frontend_deferred) throw new Error("Frontend contract generation is deferred");
  for (const [name, hash] of Object.entries(contract.files).filter(([name]) => name.startsWith("web/"))) {
    if (before[name] !== hash) throw new Error(`Stale generated contract: ${name}`);
  }
  const { build: viteBuild } = await import(pathToFileURL(requireWeb.resolve("vite")).href);
  requireProduction(process.env.NODE_ENV);
  process.env.NODE_ENV = "production";
  const result = await viteBuild({ root: WEB, configFile: path.join(WEB, "vite.config.ts"), mode: "production", envDir: false, plugins: [{ name: "exta-production-only", configResolved(config) { if (!config.isProduction || config.mode !== "production") throw new Error("Nonproduction build refused"); } }], build: { write: false } });
  if (Array.isArray(result) || !("output" in result)) throw new Error("Unexpected Vite build result");
  const modules = result.output.filter((item) => item.type === "chunk").flatMap((item) => Object.keys(item.modules));
  if (modules.some((name) => /[\\/]src[\\/]mocks[\\/]|[\\/]node_modules[\\/]msw[\\/]/.test(name))) throw new Error("Mock code reached production output");
  const files = packageOutput(result.output, await collectFontNotices());
  const artifacts = path.join(ROOT, "artifacts", "embed");
  await mkdir(artifacts, { recursive: true });
  const stage = path.join(artifacts, randomUUID());
  await mkdir(path.join(stage, "bundle"), { recursive: true });
  for (const [name, bytes] of files) {
    await mkdir(path.dirname(path.join(stage, "bundle", name)), { recursive: true });
    await writeFile(path.join(stage, "bundle", name), bytes);
  }
  const lock = JSON.parse(await readFile(path.join(WEB, "package-lock.json"), "utf8"));
  const dependencies = await collectLicenses(modules, lock, path.join(stage, "licenses"));
  dependencies.push(await collectFontLicenses(path.join(stage, "licenses")));
  const after = await sourceFiles();
  if (JSON.stringify(before) !== JSON.stringify(after)) throw new Error("Frontend sources changed during build");
  const provenance = { format: 1, node: process.versions.node, npm: npmVersion, vite: JSON.parse(await readFile(requireWeb.resolve("vite/package.json"), "utf8")).version, contract_sha256: contract.contract_sha256, manifest_sha256: digest(files.get("manifest.json")), source_files: before, dependencies };
  await writeFile(path.join(stage, "provenance.json"), JSON.stringify(provenance, null, 2) + "\n");
  const archive = stage + ".zip";
  execFileSync("go", ["run", "./tools/embedpack", stage, archive], { cwd: ROOT, env: { ...process.env, GOTOOLCHAIN: "local" }, stdio: "pipe" });
  if (JSON.stringify(before) !== JSON.stringify(await sourceFiles())) throw new Error("Frontend sources changed during archive creation");
  const destination = path.join(ROOT, "internal/webassets/production.zip");
  const previous = path.join(artifacts, `previous-${randomUUID()}.zip`);
  for (const location of [archive, destination, previous]) if (!path.resolve(location).startsWith(ROOT + path.sep)) throw new Error("Promotion path escaped workspace");
  try { await copyFile(destination, previous); } catch (error) { if (error.code !== "ENOENT") throw error; }
  await rename(archive, destination);
  console.log(`Embedded ${files.size - 1} verified UI files; ${dependencies.length} dependency licenses; previous output preserved.`);
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) await build();
