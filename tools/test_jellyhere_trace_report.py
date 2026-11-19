import unittest

from jellyhere_trace_report import summarize


class TraceReportTest(unittest.TestCase):
    def test_filters_and_gaps_use_correlated_requests(self):
        lines = [
            "2026/11/20 01:00:00 device_call trace=boot-1 device=xiaomi method=get_screen status=ok started_unix_ms=1000 total_ms=80 queue_ms=0 result_bytes=90000 image_requested=true",
            "2026/11/20 01:00:00 mcp_request trace=boot-1 client_ref=alice method=POST started_unix_ms=1000 total_ms=100",
            "2026/11/20 01:00:00 device_call trace=boot-2 device=xiaomi method=tap status=ok started_unix_ms=1300 total_ms=30 queue_ms=0 result_bytes=13 image_requested=-",
            "2026/11/20 01:00:00 mcp_request trace=boot-2 client_ref=alice method=POST started_unix_ms=1300 total_ms=50",
            "2026/11/20 01:00:00 device_call trace=boot-3 device=samsung method=get_screen status=ok started_unix_ms=1400 total_ms=20 queue_ms=0 result_bytes=500 image_requested=false",
            "2026/11/20 01:00:00 mcp_request trace=boot-3 client_ref=bob method=POST started_unix_ms=1400 total_ms=25",
        ]
        report = summarize(lines, client_ref="alice", device="xiaomi")
        self.assertEqual(report["mcp_requests"], 2)
        self.assertEqual(report["device_calls"], 2)
        self.assertEqual(report["elapsed_ms"], 350)
        self.assertEqual(report["between_calls_p50_ms"], 200)
        self.assertEqual(report["screen_with_image"], 1)
        self.assertEqual(report["screen_without_image"], 0)
        self.assertEqual(report["by_method"]["get_screen"]["result_bytes"], 90000)


if __name__ == "__main__":
    unittest.main()
