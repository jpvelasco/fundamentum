#!/usr/bin/env node
"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");

const { copyBinaries } = require("./copy-binaries.js");

function setup(dir, packages, artifacts) {
  for (const pkg of packages) {
    fs.mkdirSync(path.join(dir, "packages", pkg), { recursive: true });
  }
  for (const a of artifacts) {
    fs.mkdirSync(path.join(dir, path.dirname(a.path)), { recursive: true });
    fs.writeFileSync(path.join(dir, a.path), `${a.goos}-${a.goarch}`);
  }
}

function withTmp(fn) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "fundamentum-copy-"));
  try {
    return fn(dir);
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
}

const WIN = { type: "Binary", name: "fundamentum.exe", path: "dist/w/fundamentum.exe", goos: "windows", goarch: "amd64" };
const LINUX = { type: "Binary", name: "fundamentum", path: "dist/l/fundamentum", goos: "linux", goarch: "arm64" };
const ARCHIVE = { type: "Archive", name: "x.zip", path: "dist/x.zip", goos: "windows", goarch: "amd64" };

test("copyBinaries places each GoReleaser binary into its platform package", () => {
  withTmp((dir) => {
    const packages = ["fundamentum-cli-win32-x64", "fundamentum-cli-linux-arm64"];
    setup(dir, packages, [WIN, LINUX]);
    const copied = copyBinaries([WIN, LINUX, ARCHIVE], path.join(dir, "packages"), dir);
    assert.deepEqual(copied, ["fundamentum-cli-linux-arm64", "fundamentum-cli-win32-x64"]);
    const out = (pkg, bin) => fs.readFileSync(path.join(dir, "packages", pkg, "bin", bin), "utf8");
    assert.equal(out("fundamentum-cli-win32-x64", "fundamentum.exe"), "windows-amd64");
    assert.equal(out("fundamentum-cli-linux-arm64", "fundamentum"), "linux-arm64");
  });
});

test("copyBinaries fails when a platform package gets no binary", () => {
  withTmp((dir) => {
    setup(dir, ["fundamentum-cli-win32-x64", "fundamentum-cli-darwin-x64"], [WIN]);
    assert.throws(
      () => copyBinaries([WIN], path.join(dir, "packages"), dir),
      /no binary built for: fundamentum-cli-darwin-x64/
    );
  });
});

test("copyBinaries fails on a binary with no platform package", () => {
  withTmp((dir) => {
    setup(dir, ["fundamentum-cli-win32-x64"], [WIN, LINUX]);
    assert.throws(
      () => copyBinaries([WIN, LINUX], path.join(dir, "packages"), dir),
      /no npm package for linux\/arm64/
    );
  });
});