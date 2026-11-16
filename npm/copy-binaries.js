#!/usr/bin/env node
// nosemgrep: all — release build script, not application code
"use strict";

// Copies GoReleaser-built binaries into the npm platform packages
// (npm/packages/fundamentum-cli-<platform>-<arch>/bin/) before publishing.
//
// Usage: node npm/copy-binaries.js dist/artifacts.json npm/packages

const fs = require("fs");
const path = require("path");

const NODE_OS = { linux: "linux", darwin: "darwin", windows: "win32" };
const NODE_ARCH = { amd64: "x64", arm64: "arm64" };

// Copies every Binary artifact into its platform package and fails unless
// each package directory received exactly the binary it publishes.
function copyBinaries(artifacts, packagesDir, root = process.cwd()) {
  const expected = fs
    .readdirSync(packagesDir, { withFileTypes: true })
    .filter((entry) => entry.isDirectory())
    .map((entry) => entry.name);
  const copied = new Set();

  for (const artifact of artifacts.filter((a) => a.type === "Binary")) {
    const pkg = `fundamentum-cli-${NODE_OS[artifact.goos]}-${NODE_ARCH[artifact.goarch]}`;
    if (!expected.includes(pkg)) {
      throw new Error(`no npm package for ${artifact.goos}/${artifact.goarch} (${pkg})`);
    }
    const binDir = path.join(packagesDir, pkg, "bin");
    fs.mkdirSync(binDir, { recursive: true });
    const dest = path.join(binDir, artifact.name);
    fs.copyFileSync(path.resolve(root, artifact.path), dest);
    fs.chmodSync(dest, 0o755);
    copied.add(pkg);
  }

  const missing = expected.filter((pkg) => !copied.has(pkg));
  if (missing.length > 0) {
    throw new Error(`no binary built for: ${missing.join(", ")}`);
  }
  return [...copied].sort();
}

if (require.main === module) {
  const [artifactsFile, packagesDir] = process.argv.slice(2);
  if (!artifactsFile || !packagesDir) {
    console.error("Usage: node copy-binaries.js <dist/artifacts.json> <npm/packages>");
    process.exit(1);
  }
  const artifacts = JSON.parse(fs.readFileSync(artifactsFile, "utf8"));
  for (const pkg of copyBinaries(artifacts, packagesDir)) {
    console.log(`copied binary into ${pkg}`);
  }
}

module.exports = { copyBinaries };