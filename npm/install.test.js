#!/usr/bin/env node
"use strict";

// Unit tests for the pre-extraction member validation (zip-slip hardening, #236).
// Runs with the built-in test runner: `node --test install.test.js`.

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");

const { isPathEscape, validateMembers } = require("./install.js");

test("isPathEscape flags traversal primitives and absolute names", () => {
  assert.equal(isPathEscape(".."), true);
  assert.equal(isPathEscape("../evil"), true);
  assert.equal(isPathEscape("a/.."), true);
  assert.equal(isPathEscape("a/../../x"), true);
  assert.equal(isPathEscape("C:\\..\\x"), true);
  assert.equal(isPathEscape("/abs"), true);
});

test("isPathEscape accepts ordinary member names", () => {
  assert.equal(isPathEscape("fundamentum"), false);
  assert.equal(isPathEscape("a..b"), false);
  assert.equal(isPathEscape(".hidden"), false);
  assert.equal(isPathEscape("sub/dir/file"), false);
});

test("validateMembers rejects a member that traverses outside the dir", () => {
  assert.throws(
    () => validateMembers(["fundamentum", "../../.bashrc"]),
    /Archive path escape/
  );
  assert.throws(
    () => validateMembers(["C:\\Users\\x\\evil"]),
    /Archive path escape/
  );
});

test("validateMembers accepts a flat GoReleaser-style member list", () => {
  assert.doesNotThrow(() =>
    validateMembers([
      "fundamentum_1.2.3_linux_amd64/",
      "fundamentum_1.2.3_linux_amd64/fundamentum",
      "fundamentum_1.2.3_linux_amd64/README.md",
    ])
  );
});

test("listMembers + validateMembers reject a real tar that escapes (end to end)", () => {
  const { execFileSync } = require("node:child_process");
  const { listMembers } = require("./install.js");
  const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "fundamentum-zip-slip-"));
  const archivePath = path.join(tmpDir, "evil.tar.gz");
  const outside = path.join(path.dirname(tmpDir), "fundamentum-evil-probe");
  try {
    // Build a tar.gz whose single member is ../fundamentum-evil-probe, a
    // traversal path relative to the -C dir.
    fs.writeFileSync(outside, "x");
    execFileSync(
      "tar",
      ["-czf", archivePath, "-C", tmpDir, "../fundamentum-evil-probe"],
      { stdio: "pipe" }
    );
    const members = listMembers(archivePath, false);
    assert.ok(members.includes("../fundamentum-evil-probe"));
    assert.throws(() => validateMembers(members), /Archive path escape/);
  } finally {
    fs.rmSync(archivePath, { force: true });
    fs.rmSync(outside, { force: true });
    fs.rmSync(tmpDir, { recursive: true, force: true });
  }
});
