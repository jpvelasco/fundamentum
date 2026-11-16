#!/usr/bin/env node
"use strict";

// Unit tests for the pre-extraction member validation (zip-slip hardening, #236).
// Runs with the built-in test runner: `node --test install.test.js`.

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");

const { archiveCommands, extract, isPathEscape, validateMembers } = require("./install.js");

test("isPathEscape flags traversal primitives and absolute names", () => {
  assert.equal(isPathEscape(".."), true);
  assert.equal(isPathEscape("../evil"), true);
  assert.equal(isPathEscape("a/.."), true);
  assert.equal(isPathEscape("a/../../x"), true);
  assert.equal(isPathEscape("C:\\..\\x"), true);
  assert.equal(isPathEscape("/abs"), true);
  assert.equal(isPathEscape("C:evil"), true);
  assert.equal(isPathEscape("C:"), true);
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
  assert.throws(() => validateMembers(["C:evil"]), /Archive path escape/);
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
      // -P keeps the ../ prefix; GNU tar strips it by default.
      ["-czPf", archivePath, "-C", tmpDir, "../fundamentum-evil-probe"],
      { stdio: "pipe" }
    );
    const members = listMembers(archiveCommands(archivePath, tmpDir, false));
    assert.ok(members.includes("../fundamentum-evil-probe"));
    assert.throws(() => validateMembers(members), /Archive path escape/);
  } finally {
    fs.rmSync(archivePath, { force: true });
    fs.rmSync(outside, { force: true });
    fs.rmSync(tmpDir, { recursive: true, force: true });
  }
});

test("archiveCommands uses the OS-bundled bsdtar on Windows, never PowerShell or unzip", () => {
  const { list, extract: ex } = archiveCommands("a.zip", "out", true, "win32");
  assert.match(list[0], /System32[\\/]tar\.exe$/i);
  assert.equal(ex[0], list[0]);
  assert.deepEqual(list[1], ["-tf", "a.zip"]);
  assert.deepEqual(ex[1], ["-xf", "a.zip", "-C", "out"]);
});

test("archiveCommands keeps unzip/tar on non-Windows hosts", () => {
  assert.equal(archiveCommands("a.zip", "o", true, "linux").extract[0], "unzip");
  assert.equal(archiveCommands("a.tar.gz", "o", false, "darwin").extract[0], "tar");
});

test(
  "extract installs fundamentum.exe from a release-shaped zip on Windows (#306)",
  { skip: process.platform !== "win32" && "Windows-only extraction path" },
  () => {
    const { execFileSync } = require("node:child_process");
    const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "fundamentum-zip-"));
    try {
      const src = path.join(tmpDir, "src");
      fs.mkdirSync(src);
      fs.writeFileSync(path.join(src, "fundamentum.exe"), "binary");
      fs.writeFileSync(path.join(src, "README.md"), "readme");
      const zipPath = path.join(tmpDir, "release.zip");
      const { list } = archiveCommands(zipPath, src, true, "win32");
      execFileSync(list[0], ["-a", "-cf", zipPath, "-C", src, "fundamentum.exe", "README.md"]);

      const binDir = path.join(tmpDir, "bin");
      extract(fs.readFileSync(zipPath), "fundamentum_1.2.3_windows_amd64.zip", binDir);
      assert.equal(fs.readFileSync(path.join(binDir, "fundamentum.exe"), "utf8"), "binary");
    } finally {
      fs.rmSync(tmpDir, { recursive: true, force: true });
    }
  }
);