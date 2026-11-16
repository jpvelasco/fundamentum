#!/usr/bin/env node
// nosemgrep: all — npm run shim, not application code
"use strict";

const fs = require("fs");
const path = require("path");
const { spawn } = require("child_process");

const ISSUES_URL = "https://github.com/jpvelasco/fundamentum/issues";
const REINSTALL = "Reinstall without omitting optional dependencies: npm install -g fundamentum-cli";

function binaryName(platform) {
  return platform === "win32" ? "fundamentum.exe" : "fundamentum";
}

// The platform package must be one the launcher declares, so an arbitrary
// installed package can never be resolved and executed.
function platformPackage(platform, arch, optionalDependencies) {
  const name = `fundamentum-cli-${platform}-${arch}`;
  return Object.hasOwn(optionalDependencies || {}, name) ? name : undefined;
}

function unsupportedMessage(platform, arch) {
  if (platform === "win32" && arch === "arm64") {
    return `unsupported platform ${platform}/${arch}. Windows ARM64 binaries are not published; use Windows x64 or build from source.`;
  }
  return `unsupported platform ${platform}/${arch}. Please file an issue: ${ISSUES_URL}`;
}

function readVersion(pkgJsonPath) {
  try {
    return JSON.parse(fs.readFileSync(pkgJsonPath, "utf8")).version;
  } catch (_) {
    return undefined;
  }
}

// Returns the absolute path of the binary to run, or throws with a recovery
// message. A 0.0.0 launcher is a git checkout and runs a local build instead.
function locateBinary({ platform, arch, pkg, dir = __dirname, resolve = require.resolve }) {
  const name = platformPackage(platform, arch, pkg.optionalDependencies);
  if (!name) {
    throw new Error(unsupportedMessage(platform, arch));
  }
  const exe = binaryName(platform);

  if (pkg.version === "0.0.0") {
    const local = path.join(dir, "bin", exe);
    if (!fs.existsSync(local)) {
      throw new Error(
        "package version is 0.0.0 (git checkout) and no local build exists.\n" +
          `Build locally: go build -o npm/bin/${exe} .\n` +
          "Or install a published release: npm install -g fundamentum-cli"
      );
    }
    return local;
  }

  let pkgDir;
  try {
    pkgDir = path.dirname(resolve(`${name}/package.json`, { paths: [dir] }));
  } catch (_) {
    throw new Error(`platform package ${name} is not installed.\n${REINSTALL}`);
  }

  const binVersion = readVersion(path.join(pkgDir, "package.json"));
  if (binVersion !== pkg.version) {
    throw new Error(
      `launcher v${pkg.version} does not match binary package v${binVersion}.\n${REINSTALL}`
    );
  }

  const bin = path.join(pkgDir, "bin", exe);
  if (!fs.existsSync(bin)) {
    throw new Error(`binary not found at ${bin}.\n${REINSTALL}`);
  }
  return bin;
}

function runBinary(bin) {
  const child = spawn(bin, process.argv.slice(2), { stdio: "inherit" });

  // Forward signals so the Go binary shuts down cleanly
  ["SIGINT", "SIGTERM", "SIGHUP"].forEach((sig) => {
    process.on(sig, () => child.kill(sig));
  });

  child.on("error", (err) => {
    console.error(`fundamentum: failed to start ${bin}: ${err.message}`);
    process.exit(1);
  });

  child.on("exit", (code, signal) => {
    process.exit(signal ? 1 : code || 0);
  });
}

if (require.main === module) {
  let bin;
  try {
    bin = locateBinary({
      platform: process.platform,
      arch: process.arch,
      pkg: require("./package.json"),
    });
  } catch (err) {
    console.error(`fundamentum: ${err.message}`);
    process.exit(1);
  }
  runBinary(bin);
}

module.exports = { locateBinary, platformPackage };