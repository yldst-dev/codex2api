import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

from update_codex_version import (
    current_version,
    replace_oauth,
    replace_readme,
    version_from_release,
)


OAUTH = '''const (
	UserAgent  = "codex-tui/0.155.1 (Ubuntu 22.4.0; x86_64) xterm-256color"
	Originator = "codex-tui"
	Version    = "0.155.1"
	MinVersion = "0.144.0"
)
'''

README = """User-Agent: codex-tui/0.155.1 (Ubuntu 22.4.0; x86_64) xterm-256color
originator: codex-tui
version: 0.155.1
"""


class UpdateCodexVersionTest(unittest.TestCase):
    def test_release_name(self):
        self.assertEqual(version_from_release({"name": "0.155.1", "tag_name": "rust-v0.155.1"}), "0.155.1")

    def test_release_tag_fallback(self):
        self.assertEqual(version_from_release({"name": "Codex", "tag_name": "rust-v0.156.0"}), "0.156.0")

    def test_alpha_rejected(self):
        with self.assertRaises(ValueError):
            version_from_release({"name": "0.157.0-alpha.9", "tag_name": "rust-v0.157.0-alpha.9"})

    def test_replace_keeps_floor(self):
        updated = replace_oauth(OAUTH, "0.156.0")
        self.assertIn('UserAgent  = "codex-tui/0.156.0 ', updated)
        self.assertIn('Version    = "0.156.0"', updated)
        self.assertIn('MinVersion = "0.144.0"', updated)
        self.assertEqual(current_version(updated), "0.156.0")

    def test_replace_readme_once(self):
        updated = replace_readme(README, "0.156.0")
        self.assertEqual(updated.count("0.156.0"), 2)
        self.assertNotIn("0.155.1", updated)

    def test_apply_on_copies(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "internal/oauth").mkdir(parents=True)
            (root / "internal/oauth/oauth.go").write_text(OAUTH, encoding="utf-8")
            (root / "README.md").write_text(README, encoding="utf-8")
            import subprocess
            import sys

            completed = subprocess.run(
                [sys.executable, str(Path(__file__).with_name("update_codex_version.py")), "--version", "0.156.2", "--root", str(root)],
                check=False,
                capture_output=True,
                text=True,
            )
            self.assertEqual(completed.returncode, 0, completed.stderr)
            self.assertIn("0.156.2", (root / "internal/oauth/oauth.go").read_text(encoding="utf-8"))
            self.assertIn("version: 0.156.2", (root / "README.md").read_text(encoding="utf-8"))
            self.assertIn('MinVersion = "0.144.0"', (root / "internal/oauth/oauth.go").read_text(encoding="utf-8"))


if __name__ == "__main__":
    unittest.main()
