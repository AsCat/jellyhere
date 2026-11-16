import io
import unittest
from unittest.mock import patch

import jellyhere_cli


class JellyHereCLITest(unittest.TestCase):
    def test_adb_command_uses_existing_helper_and_device_id(self):
        with patch.object(jellyhere_cli, "adb_main", return_value=0) as helper:
            self.assertEqual(0, jellyhere_cli.main(["adb", "connect", "--device", "samsung", "--no-adb"]))
        helper.assert_called_once_with(["--device", "samsung", "--no-adb"],
                                       command_name="jellyhere adb connect")

    def test_device_list_is_read_only_helper_mode(self):
        with patch.object(jellyhere_cli, "adb_main", return_value=0) as helper:
            self.assertEqual(0, jellyhere_cli.main(["device", "list"]))
        helper.assert_called_once_with(["--list"], command_name="jellyhere device list")

    def test_doctor_does_not_print_token(self):
        class FakeClient:
            def __init__(self, url, token, device_id):
                self.token = token

            def call(self, tool):
                return [{"type": "text", "text": '[{"id":"primary","online":true}]'}]

        output = io.StringIO()
        with patch.object(jellyhere_cli, "load_url", return_value="https://example.com/mcp"), \
             patch.object(jellyhere_cli, "load_token", return_value="private-token"), \
             patch.object(jellyhere_cli, "OrbClient", FakeClient), \
             patch.object(jellyhere_cli.shutil, "which", return_value=None), \
             patch("sys.stdout", output):
            self.assertEqual(0, jellyhere_cli.main(["doctor"]))
        self.assertIn("primary", output.getvalue())
        self.assertNotIn("private-token", output.getvalue())


if __name__ == "__main__":
    unittest.main()
