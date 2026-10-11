import json
import unittest
from types import SimpleNamespace
from unittest.mock import Mock

from bench.config import MIN_BITRATE_BPS, MIN_FPS
from bench.experiment import Experiment


class ReceiverHealthTest(unittest.TestCase):
    def check_health(self, expected, reported, fps=MIN_FPS):
        ssh = Mock()
        ssh.run.side_effect = [
            SimpleNamespace(stdout=f"eth1 0 {expected * MIN_BITRATE_BPS * 5 // 8}"),
            SimpleNamespace(stdout=json.dumps({
                str(i): {"bitrate_bps": MIN_BITRATE_BPS, "fps": fps}
                for i in range(reported)
            })),
        ]
        experiment = Experiment(ssh, {
            "sender": "sender", "sfu": ["sfu"], "receivers": ["worker"],
        }, "livekit", "rtcbench", expected, ".")
        return experiment._check_health(), experiment._health_reason

    def test_complete_small_runs(self):
        for viewers in (1, 5, 20, 100):
            with self.subTest(viewers=viewers):
                self.assertEqual(self.check_health(viewers, viewers), (True, "OK"))

    def test_missing_viewers(self):
        for expected, reported in ((5, 4), (100, 50)):
            with self.subTest(expected=expected):
                self.assertEqual(self.check_health(expected, reported), (False, "TOO_FEW_VIEWERS"))

    def test_no_stats(self):
        self.assertEqual(self.check_health(5, 0), (False, "NO_VIEWERS"))

    def test_poor_quality(self):
        self.assertEqual(self.check_health(5, 5, fps=0), (False, "QUALITY_DEGRADED"))
