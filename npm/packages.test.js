#!/usr/bin/env node
"use strict";

// The launcher's optionalDependencies, the platform package directories, and
// each package's os/cpu constraints must agree, or npm installs a package
// whose binary the launcher never resolves.

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

function readPackage(file) {
  return JSON.parse(fs.readFileSync(file, "utf8"));
}

const main = readPackage(path.join(__dirname, "package.json"));
const packagesDir = path.join(__dirname, "packages");

test("platform packages match the launcher's optional dependencies", () => {
  const dirs = fs.readdirSync(packagesDir).sort();
  assert.deepEqual(dirs, Object.keys(main.optionalDependencies).sort());

  for (const dir of dirs) {
    const pkg = readPackage(path.join(packagesDir, dir, "package.json"));
    const [, platform, arch] = dir.match(/^fundamentum-cli-([a-z0-9]+)-([a-z0-9]+)$/);
    assert.equal(pkg.name, dir);
    assert.equal(pkg.version, main.optionalDependencies[dir]);
    assert.deepEqual(pkg.os, [platform]);
    assert.deepEqual(pkg.cpu, [arch]);
    assert.deepEqual(pkg.files, ["bin/"]);
    assert.ok(main.os.includes(platform) && main.cpu.includes(arch), `${dir} outside launcher os/cpu`);
  }
});