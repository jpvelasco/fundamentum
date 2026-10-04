#!/usr/bin/env node
"use strict";

// Unit tests for the post-extraction path validation (zip-slip hardening, #236).
// Runs with the built-in test runner: `node --test install.test.js`.

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");

const { validateExtraction, isPathEscape } = require("./install.js");

function makeTempDir() {
  return fs.mkdtempSync(path.join(os.tmpdir(), "fundamentum-zip-slip-"));
}

test("isPathEscape flags traversal primitives and absolute names", () => {
  assert.equal(isPathEscape(".."), true);
  assert.equal(isPathEscape("../evil"), true);
  assert.equal(isPathEscape("a/.."), true);
  assert.equal(isPathEscape("C:\\..\\x"), true);
  assert.equal(isPathEscape("/abs"), true);
});

test("isPathEscape accepts ordinary member names", () => {
  assert.equal(isPathEscape("fundamentum"), false);
  assert.equal(isPathEscape("a..b"), false);
  assert.equal(isPathEscape(".hidden"), false);
  assert.equal(isPathEscape("sub/dir"), false);
});

test("validateExtraction accepts a flat layout that stays inside the temp dir", () => {
  const tmpDir = makeTempDir();
  try {
    fs.mkdirSync(path.join(tmpDir, "sub"));
    fs.writeFileSync(path.join(tmpDir, "fundamentum"), "x");
    fs.writeFileSync(path.join(tmpDir, "sub", "README"), "y");
    assert.doesNotThrow(() => validateExtraction(tmpDir));
  } finally {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  }
});

test("validateExtraction accepts the temp dir passed as a relative path", () => {
  const tmpDir = makeTempDir();
  const cwd = process.cwd();
  try {
    fs.mkdirSync(path.join(tmpDir, "a"), { recursive: true });
    process.chdir(path.dirname(tmpDir));
    const rel = path.relative(path.dirname(tmpDir), tmpDir);
    assert.doesNotThrow(() => validateExtraction(rel));
  } finally {
    process.chdir(cwd);
    fs.rmSync(tmpDir, { recursive: true, force: true });
  }
});
