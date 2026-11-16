#!/usr/bin/env node
"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");

const { locateBinary, platformPackage } = require("./run.js");

const DEPS = { "fundamentum-cli-linux-x64": "1.2.3", "fundamentum-cli-win32-x64": "1.2.3" };
const PKG = { version: "1.2.3", optionalDependencies: DEPS };

function withTmp(fn) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "fundamentum-run-"));
  try {
    return fn(dir);
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
}

// Lays out an installed platform package and returns a resolver for it.
function installPlatform(dir, name, version, binary) {
  const pkgDir = path.join(dir, "node_modules", name);
  fs.mkdirSync(path.join(pkgDir, "bin"), { recursive: true });
  fs.writeFileSync(path.join(pkgDir, "package.json"), JSON.stringify({ name, version }));
  if (binary) {
    fs.writeFileSync(path.join(pkgDir, "bin", binary), "bin");
  }
  return (request) => {
    if (request !== `${name}/package.json`) {
      throw new Error(`Cannot find module '${request}'`);
    }
    return path.join(pkgDir, "package.json");
  };
}

test("platformPackage only returns declared optional dependencies", () => {
  assert.equal(platformPackage("linux", "x64", DEPS), "fundamentum-cli-linux-x64");
  assert.equal(platformPackage("freebsd", "x64", DEPS), undefined);
  assert.equal(platformPackage("linux", "x64", undefined), undefined);
  assert.equal(platformPackage("constructor", "", {}), undefined);
});

test("locateBinary resolves the binary from the matching platform package", () => {
  withTmp((dir) => {
    const resolve = installPlatform(dir, "fundamentum-cli-win32-x64", "1.2.3", "fundamentum.exe");
    const bin = locateBinary({ platform: "win32", arch: "x64", pkg: PKG, dir, resolve });
    assert.equal(bin, path.join(dir, "node_modules", "fundamentum-cli-win32-x64", "bin", "fundamentum.exe"));
  });
});

test("locateBinary refuses a launcher/binary version skew", () => {
  withTmp((dir) => {
    const resolve = installPlatform(dir, "fundamentum-cli-linux-x64", "1.2.2", "fundamentum");
    assert.throws(
      () => locateBinary({ platform: "linux", arch: "x64", pkg: PKG, dir, resolve }),
      /launcher v1\.2\.3 does not match binary package v1\.2\.2/
    );
  });
});

test("locateBinary reports a missing platform package or binary", () => {
  withTmp((dir) => {
    const resolve = installPlatform(dir, "fundamentum-cli-linux-x64", "1.2.3", null);
    assert.throws(
      () => locateBinary({ platform: "win32", arch: "x64", pkg: PKG, dir, resolve }),
      /fundamentum-cli-win32-x64 is not installed/
    );
    assert.throws(
      () => locateBinary({ platform: "linux", arch: "x64", pkg: PKG, dir, resolve }),
      /binary not found/
    );
  });
});

test("locateBinary rejects unsupported platforms with a specific Windows ARM64 message", () => {
  assert.throws(
    () => locateBinary({ platform: "win32", arch: "arm64", pkg: PKG }),
    /Windows ARM64 binaries are not published/
  );
  assert.throws(
    () => locateBinary({ platform: "freebsd", arch: "x64", pkg: PKG }),
    /unsupported platform freebsd\/x64\. Please file an issue/
  );
});

test("locateBinary uses a local build in a 0.0.0 git checkout", () => {
  withTmp((dir) => {
    const pkg = { version: "0.0.0", optionalDependencies: DEPS };
    assert.throws(
      () => locateBinary({ platform: "linux", arch: "x64", pkg, dir }),
      /go build -o npm\/bin\/fundamentum \./
    );
    fs.mkdirSync(path.join(dir, "bin"));
    fs.writeFileSync(path.join(dir, "bin", "fundamentum"), "bin");
    assert.equal(
      locateBinary({ platform: "linux", arch: "x64", pkg, dir }),
      path.join(dir, "bin", "fundamentum")
    );
  });
});