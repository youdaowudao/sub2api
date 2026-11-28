"""Exercise RED test-range guards with real Git history, without running Go."""
import contextlib
import io
import os
import pathlib
import subprocess
import tempfile
import unittest
from unittest import mock

import red_check


class RedTestSourceTests(unittest.TestCase):
    def setUp(self):
        root = pathlib.Path(__file__).resolve().parents[1]
        scratch = root / ".local" / "heavy-validation" / "tmp"
        scratch.mkdir(parents=True, exist_ok=True)
        self.temp = tempfile.TemporaryDirectory(prefix="red-check-test-", dir=scratch)
        self.addCleanup(self.temp.cleanup)
        self.repo = pathlib.Path(self.temp.name)
        previous = pathlib.Path.cwd()
        os.chdir(self.repo)
        self.addCleanup(os.chdir, previous)
        self.env = dict(os.environ, GIT_AUTHOR_NAME="RED fixture", GIT_COMMITTER_NAME="RED fixture",
                        GIT_AUTHOR_EMAIL="red@example.invalid", GIT_COMMITTER_EMAIL="red@example.invalid")
        self.git("init", "-q", "-b", "fixture")
        self.write("backend/internal/service/core.go", "package service\n")
        self.write("backend/internal/service/obsolete_test.go", "old test\n")
        self.base = self.commit("base")
        self.git("update-ref", "refs/remotes/origin/main", self.base)
        (self.repo / "backend/internal/service/obsolete_test.go").unlink()
        self.regression = "backend/internal/service/regression_test.go"
        self.fixture = "backend/internal/service/testdata/input.json"
        self.official = "backend/internal/service/official_new_test.go"
        self.write(self.regression, "regression test unchanged after upstream import\n")
        self.write(self.fixture, '{"input":"same"}\n')
        self.source = self.commit("regression\n\nRED: ./internal/service TestRegression")
        self.write(self.official, "official test requiring newly imported production code\n")
        self.git("add", ".")
        tree = self.git("write-tree")
        upstream = self.git("commit-tree", tree, "-p", self.base, "-m", "upstream")
        self.head = self.git("commit-tree", tree, "-p", self.source, "-p", upstream,
                             "-m", "merge\n\nRED-TEST-SOURCE: " + self.source)
        self.git("update-ref", "HEAD", self.head, self.source)
        self.commits = self.git("rev-list", "--topo-order", self.base + ".." + self.head).splitlines()
        self.targets = {("./internal/service", "TestRegression"): self.commits.index(self.source)}
        self.declarations = [(self.head, self.source)]

    def git(self, *args):
        return subprocess.run(["git", *args], env=self.env, check=True, capture_output=True, text=True).stdout.strip()

    def write(self, path, text):
        target = self.repo / path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(text)

    def commit(self, message):
        self.git("add", "-A")
        self.git("commit", "-q", "-m", message)
        return self.git("rev-parse", "HEAD")

    def resolve(self, declarations=None, base=None, targets=None):
        return red_check.resolve_test_source(base or self.base, self.head, self.commits,
                                            self.targets if targets is None else targets,
                                            self.declarations if declarations is None else declarations)

    def test_default_keeps_current_head_and_all_changed_tests(self):
        self.assertEqual(self.head, self.resolve([]))
        removed, restored = red_check.test_changes(self.base, self.head, self.head, {"./internal/service"})
        self.assertIn(self.official, restored)
        self.assertIn(self.regression, restored)
        self.assertIn("backend/internal/service/obsolete_test.go", removed)

    def test_import_snapshot_preserves_regression_and_fixture_but_excludes_new_official_tests(self):
        self.assertEqual(self.source, self.resolve())
        removed, restored = red_check.test_changes(self.base, self.head, self.source, {"./internal/service"})
        self.assertEqual({self.regression, self.fixture}, set(restored))
        self.assertNotIn(self.official, restored)
        self.assertEqual(["backend/internal/service/obsolete_test.go"], removed)

    def test_bad_or_duplicate_marker_is_rejected(self):
        for declarations in [[(self.head, "bad")], self.declarations * 2]:
            with self.subTest(declarations=declarations), self.assertRaises(ValueError):
                self.resolve(declarations)

    def test_marker_must_belong_to_a_merge_and_match_its_first_parent(self):
        for declarations in [[(self.source, self.base)], [(self.head, self.base)]]:
            with self.subTest(declarations=declarations), self.assertRaises(ValueError):
                self.resolve(declarations)

    def test_snapshot_cannot_precede_production_baseline(self):
        with self.assertRaises(ValueError):
            self.resolve(base=self.head)

    def test_new_target_after_snapshot_is_rejected(self):
        targets = {("./internal/service", "TestNewRegression"): self.commits.index(self.head)}
        with self.assertRaises(ValueError):
            self.resolve(targets=targets)

    def test_changed_regression_bytes_are_rejected(self):
        self.write(self.regression, "changed regression assertions\n")
        head = self.commit("change regression")
        with self.assertRaises(ValueError):
            red_check.test_changes(self.base, head, self.source, {"./internal/service"})

    def test_changed_fixture_bytes_are_rejected(self):
        self.write(self.fixture, '{"input":"changed"}\n')
        head = self.commit("change fixture")
        with self.assertRaises(ValueError):
            red_check.test_changes(self.base, head, self.source, {"./internal/service"})

    def test_changed_file_mode_is_rejected(self):
        (self.repo / self.regression).chmod(0o755)
        head = self.commit("change mode")
        with self.assertRaises(ValueError):
            red_check.test_changes(self.base, head, self.source, {"./internal/service"})

    def test_deleted_selected_file_is_rejected(self):
        (self.repo / self.regression).unlink()
        head = self.commit("delete regression")
        with self.assertRaises(ValueError):
            red_check.test_changes(self.base, head, self.source, {"./internal/service"})

    def test_invalid_marker_fails_even_when_there_are_no_red_targets(self):
        self.write("note.txt", "invalid second marker\n")
        self.commit("invalid marker\n\nRED-TEST-SOURCE: bad")
        with mock.patch.object(red_check, "red_lines", return_value=[]), \
                mock.patch.object(red_check, "run_target") as run_target, \
                contextlib.redirect_stdout(io.StringIO()) as output:
            self.assertEqual(1, red_check.main())
        self.assertIn("::error::", output.getvalue())
        run_target.assert_not_called()


if __name__ == "__main__":
    unittest.main()
