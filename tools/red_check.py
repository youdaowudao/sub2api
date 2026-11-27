#!/usr/bin/env python3
"""RED 回归核对：在同一次 CI 里证明新测试在旧代码上失败、在新代码上通过。

规则（写在提交说明里，一行一条；本批各提交的 RED 行累加）：
  RED: <包路径> <测试名> [<测试名> ...]   例：RED: ./internal/handler TestFoo TestBar/case_a
  RED: 无 <理由>                           该修复不适合 RED 时写明理由，由代码 Review 核对
  RED: 撤回 <包路径> <测试名> [...]        撤回之前写错的 RED 目标（不能改写历史，只能追加撤回）
  - 包路径必须是单个包，如 ./internal/handler，不接受 ./... 这类多包写法。
  - 每个改动 backend Go 代码的“修复:”或“fix:”提交，本身或之后的提交里必须有仍然有效的 RED 目标或豁免。
  - 格式写错的 RED 行只警告、不计入，在新提交里写正确的行即可；撤回后可以重新声明。
  - 测试被跳过不算通过，也不改用集成测试；只有单元测试里找不到该测试时才改用集成测试。

判定：
  - 旧代码 = 本分支与 origin/main 的分叉点，只把目标包目录下本批改动的 _test.go 和该包 testdata/ 换成新版本；
    指定测试必须实际执行且自身报告失败。编译失败、未执行、跳过、只有包级失败都不算 RED。
  - 新代码 = 当前提交；指定测试必须实际执行并通过，且 go test 正常退出。
  - 没有 RED 行且不需要时，输出“不适用”并通过。
"""
import json
import os
import pathlib
import re
import shutil
import subprocess
import sys
import tempfile

MODULE = "backend"
TAGS = ("unit", "integration")
NO_FILES = re.compile(r"build constraints exclude all Go files in (.+?)\s*$", re.MULTILINE)
PKG = re.compile(r"^\./[A-Za-z0-9_\-]+(/[A-Za-z0-9_\-]+)*$")


def git(*args: str) -> str:
    return subprocess.run(["git", *args], check=True, capture_output=True, text=True).stdout


def git_z(*args: str) -> list[str]:
    # -z 输出按字节读取再拆分，避免文本模式改写路径里的回车等字符。
    out = subprocess.run(["git", *args], check=True, capture_output=True).stdout
    return [p.decode("utf-8", "surrogateescape") for p in out.split(b"\0") if p]


def anchored(name: str) -> str:
    # 每一级子测试分别锚定并转义，避免前缀相同的其他测试被一起选中。
    return "/".join(f"^{re.escape(part)}$" for part in name.split("/"))


def red_lines(message: str) -> list[str]:
    return [line[4:].strip() for line in message.splitlines() if line.startswith("RED:")]


def run_target(module_dir: pathlib.Path, pkg: str, name: str, tag: str) -> tuple[str, int, str]:
    """返回 (目标测试结果, 进程退出码, 日志尾部)。结果为 pass / fail / skipped / not-run / build-failed。"""
    proc = subprocess.run(
        ["go", "test", f"-tags={tag}", "-count=1", "-json", "-run", anchored(name), pkg],
        cwd=module_dir, capture_output=True, text=True,
    )
    result, output, parent_skipped = "not-run", [], False
    parts = name.split("/")
    ancestors = {"/".join(parts[:k]) for k in range(1, len(parts))}
    for line in proc.stdout.splitlines():
        try:
            event = json.loads(line)
        except json.JSONDecodeError:
            output.append(line)
            continue
        if event.get("Output"):
            output.append(event["Output"].rstrip("\n"))
        if event.get("Test") == name and event.get("Action") in ("pass", "fail", "skip"):
            result = {"pass": "pass", "fail": "fail", "skip": "skipped"}[event["Action"]]
        if event.get("Test") in ancestors and event.get("Action") == "skip":
            parent_skipped = True
    if result == "not-run" and parent_skipped:
        result = "skipped"  # 父测试跳过导致子测试没有运行，按跳过处理，不改用集成测试
    text = "\n".join(output + proc.stderr.splitlines())
    # 只有目标包自身因构建标签没有可编译文件时才算“找不到”，依赖包编译失败仍算编译失败。
    target_dir = (module_dir / pkg[2:]).resolve()
    own_no_files = any(pathlib.Path(m).resolve() == target_dir for m in NO_FILES.findall(text))
    if result == "not-run" and proc.returncode != 0 and not own_no_files:
        result = "build-failed"
    return result, proc.returncode, "\n".join(text.splitlines()[-25:])


def changed_go(sha: str) -> bool:
    # 合并提交按第一父提交比较。
    files = git_z("diff", "--name-only", "--no-renames", "-z", f"{sha}^1", sha)
    return any(f.startswith(f"{MODULE}/") and f.endswith(".go") for f in files)


def main() -> int:
    base = git("merge-base", "origin/main", "HEAD").strip()
    head = git("rev-parse", "HEAD").strip()
    commits = git("rev-list", "--topo-order", f"{base}..{head}").split()  # 新到旧，保证子提交在父提交之前
    print(f"基线（分叉点）: {base}\n当前提交: {head}")

    # 按提交从旧到新维护当前有效声明：目标 -> 最近一次声明它的提交序号（序号越小越新），撤回即移除。
    # 格式错误的行只警告、不计入；最后统一核对每个修复提交是否被有效目标或豁免覆盖。
    targets: dict[tuple[str, str], int] = {}
    waivers: list[int] = []
    for i in reversed(range(len(commits))):
        sha = commits[i]
        for entry in red_lines(git("log", "-1", "--format=%B", sha)):
            parts = entry.split()
            if parts[:1] == ["无"]:
                if len(parts) < 2:
                    print(f"::warning::{sha[:9]} 的 'RED: 无' 没有写理由，不计入")
                    continue
                waivers.append(i)
                print(f"RED 豁免（{sha[:9]}）：{' '.join(parts[1:])}")
                continue
            withdraw = parts[:1] == ["撤回"]
            if withdraw:
                parts = parts[1:]
            if len(parts) < 2 or not PKG.match(parts[0]):
                print(f"::warning::{sha[:9]} 的 RED 行格式错误，不计入：{entry!r}（应为 'RED: ./<单个包> <测试名>...'）")
                continue
            for name in parts[1:]:
                key = (parts[0], name)
                if withdraw:
                    if targets.pop(key, None) is None:
                        print(f"::warning::{sha[:9]} 撤回的 {key[0]} {key[1]} 当前没有生效的声明")
                    else:
                        print(f"撤回（{sha[:9]}）：{key[0]} {key[1]}")
                else:
                    targets[key] = i

    # 每个改动 Go 代码的修复提交，本身或之后（更新）的提交里必须有仍然有效的 RED 目标或豁免。
    for i, sha in enumerate(commits):
        subject = git("log", "-1", "--format=%s", sha).strip()
        if not (subject.startswith(("修复:", "fix:")) and changed_go(sha)):
            continue
        if not any(j <= i for j in list(targets.values()) + waivers):
            print(f"::error::修复提交 {sha[:9]} “{subject}” 及之后的提交里没有有效的 RED 目标或豁免。")
            print("请在新提交说明里写 'RED: ./<包> <测试名>'，或 'RED: 无 <理由>'。")
            return 1

    if not targets:
        print("不适用：本批没有需要核对的 RED 测试。")
        return 0

    raw = git_z("diff", "--name-status", "--no-renames", "-z", base, head)
    changed = [(raw[i], raw[i + 1]) for i in range(0, len(raw) - 1, 2)]  # (状态, 路径)，改名按删除+新增
    changed.sort(key=lambda c: c[0] != "D")  # 先处理删除，再写入新增和修改
    root = pathlib.Path(git("rev-parse", "--show-toplevel").strip())
    old_dir = pathlib.Path(tempfile.mkdtemp(prefix="red-old-"))
    failures = []
    try:
        git("worktree", "add", "--detach", str(old_dir), base)
        removed, restored = [], []
        for pkg in sorted({p for p, _ in targets}):
            pkg_prefix = f"{MODULE}/{pkg[2:].rstrip('/')}/"
            for status, path in changed:
                rel = path[len(pkg_prefix):] if path.startswith(pkg_prefix) else None
                # 只换目标包自己的测试文件（不含子包）和该包 testdata/
                if rel is None or not ((rel.endswith("_test.go") and "/" not in rel) or rel == "testdata" or rel.startswith("testdata/")):
                    continue
                (removed if status == "D" else restored).append(path)
        # 用 git 自己删除和恢复，文件类型、权限、符号链接、目录与文件互换都与新版本一致
        # --literal-pathspecs：路径按字面处理，避免 [ ] * 之类字符被当成通配符
        if removed:
            git("--literal-pathspecs", "-C", str(old_dir), "rm", "-q", "-f", "--", *removed)
        # 删除的路径在换入前核对已不存在（换入后同名处可能是新版本的目录或链接）
        leftover = [p for p in removed if os.path.lexists(old_dir / p)]
        if restored:
            git("--literal-pathspecs", "-C", str(old_dir), "checkout", head, "--", *restored)
        # 自检：旧代码一侧这些路径必须与新版本完全一致（含文件类型和权限），否则 RED 结论不可信
        if removed or restored:
            # 只核对实际换入的文件；删除的路径只核对旧代码一侧已不存在（新版本同名处可能是目录，不展开核对）
            check = subprocess.run(["git", "--literal-pathspecs", "-C", str(old_dir), "diff", "--quiet", head, "--", *restored]) if restored else None
            if (check is not None and check.returncode != 0) or leftover:
                print("::error::旧代码一侧换入的测试文件与新版本不一致，无法给出 RED 结论")
                return 1
        for path in removed:
            print(f"旧代码一侧去掉本批删除的测试文件：{path}")
        for path in restored:
            print(f"旧代码一侧换入新测试文件：{path}")

        for pkg, name in targets:
            for tag in TAGS:
                new_result, new_code, new_tail = run_target(root / MODULE, pkg, name, tag)
                if new_result != "not-run":
                    break
            old_result, _, old_tail = run_target(old_dir / MODULE, pkg, name, tag)
            red_ok = old_result == "fail"
            green_ok = new_result == "pass" and new_code == 0
            print(f"{'通过' if red_ok and green_ok else '不通过'}  {pkg} {name}（-tags={tag}）：旧代码={old_result}，新代码={new_result}，新代码退出码={new_code}")
            if not red_ok:
                reason = {"pass": "新测试在旧代码上也通过，没有证明能抓到这个 bug",
                          "skipped": "旧代码上该测试被跳过，不能算 RED",
                          "not-run": "旧代码上没有执行到该测试（名称写错）",
                          "build-failed": "旧代码上编译失败（测试依赖了本批新增的业务代码），不能算 RED"}[old_result]
                failures.append(f"{pkg} {name}：{reason}\n{old_tail}")
            if not green_ok:
                failures.append(f"{pkg} {name}：新代码上结果为 {new_result}、退出码 {new_code}，应为通过且退出码 0\n{new_tail}")
    finally:
        subprocess.run(["git", "worktree", "remove", "--force", str(old_dir)], capture_output=True)
        shutil.rmtree(old_dir, ignore_errors=True)

    for f in failures:
        print(f"::error::{f.splitlines()[0]}")
        print(f)
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
