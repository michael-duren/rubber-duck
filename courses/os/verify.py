#!/usr/bin/env python3
"""Verify every challenge in a course section markdown file.

Usage: verify.py <section.md> [workdir]

For each challenge (## Challenge: ... {#slug points=N} or
# Final Challenge: ... {#slug points=N}):
  1. Extract the Starter block -> <workdir>/<slug>/solution.c
     and the Tests block      -> <workdir>/<slug>/test_solution.c
  2. cc -std=c17 -Wall -Wextra -O1: must compile with zero warnings.
  3. Run the starter binary: must exit non-zero, not crash (no signal),
     and print at least one "--- FAIL:".
  4. If refsols/<slug>.c exists next to this script, compile it against
     the tests: zero warnings, all "--- PASS:", exit 0.
Exit code: number of failed checks.
"""
import os
import re
import subprocess
import sys

md_path = sys.argv[1]
workdir = sys.argv[2] if len(sys.argv) > 2 else "/tmp/claude-1000/-home-mduren-Code-rd-wt-os-project/5bdbf553-ca5e-46ed-b612-8e97a2316d75/scratchpad/verify"
refsol_dir = os.path.join(os.path.dirname(os.path.abspath(md_path)), "refsols")

with open(md_path) as f:
    lines = f.readlines()

challenges = []  # (slug, starter_lines, tests_lines)
slug = None
mode = None
in_block = False
buf = []
blocks = {}

for line in lines:
    m = re.match(r"^#{1,2} (?:Final )?Challenge: .*\{#([a-z0-9-]+) points=\d+\}", line)
    if m:
        if slug:
            challenges.append((slug, blocks.get("starter"), blocks.get("tests")))
        slug = m.group(1)
        blocks = {}
        continue
    if line.startswith("### Starter"):
        mode = "starter"
        continue
    if line.startswith("### Tests"):
        mode = "tests"
        continue
    if mode and not in_block and line.strip() == "```c":
        in_block = True
        buf = []
        continue
    if in_block and line.strip() == "```":
        blocks[mode] = buf
        in_block = False
        mode = None
        continue
    if in_block:
        buf.append(line)
if slug:
    challenges.append((slug, blocks.get("starter"), blocks.get("tests")))

failed = 0

def check(ok, name):
    global failed
    print(("PASS  " if ok else "FAIL  ") + name)
    if not ok:
        failed += 1

for slug, starter, tests in challenges:
    d = os.path.join(workdir, slug)
    os.makedirs(d, exist_ok=True)
    if not starter or not tests:
        check(False, f"{slug}: has Starter and Tests blocks")
        continue
    with open(os.path.join(d, "solution.c"), "w") as f:
        f.writelines(starter)
    with open(os.path.join(d, "test_solution.c"), "w") as f:
        f.writelines(tests)

    cc = subprocess.run(
        ["cc", "-std=c17", "-Wall", "-Wextra", "-O1", "-o", "test_bin",
         "solution.c", "test_solution.c"],
        cwd=d, capture_output=True, text=True)
    clean = cc.returncode == 0 and cc.stderr.strip() == ""
    check(clean, f"{slug}: starter compiles, zero warnings")
    if not clean:
        print(cc.stderr[:3000])
        continue

    run = subprocess.run(["./test_bin"], cwd=d, capture_output=True,
                         text=True, timeout=30)
    check(0 < run.returncode < 128, f"{slug}: starter exits non-zero, no crash (rc={run.returncode})")
    check("--- FAIL:" in run.stdout, f"{slug}: starter prints a FAIL line")

    ref = os.path.join(refsol_dir, slug + ".c")
    if not os.path.exists(ref):
        check(False, f"{slug}: refsol exists ({ref})")
        continue
    cc2 = subprocess.run(
        ["cc", "-std=c17", "-Wall", "-Wextra", "-O1", "-o", "ref_bin",
         ref, "test_solution.c"],
        cwd=d, capture_output=True, text=True)
    clean2 = cc2.returncode == 0 and cc2.stderr.strip() == ""
    check(clean2, f"{slug}: refsol compiles, zero warnings")
    if not clean2:
        print(cc2.stderr[:3000])
        continue
    run2 = subprocess.run(["./ref_bin"], cwd=d, capture_output=True,
                          text=True, timeout=30)
    ntests = run2.stdout.count("--- PASS:") + run2.stdout.count("--- FAIL:")
    ok2 = run2.returncode == 0 and "--- FAIL:" not in run2.stdout and ntests > 0
    check(ok2, f"{slug}: refsol passes all {ntests} tests, exit 0")
    if not ok2:
        print(run2.stdout[-2000:])

sys.exit(failed)
