import unittest
from workflow_summary import STAGES, release_summary


class ReleaseSummaryTest(unittest.TestCase):
    def test_partial_alias_failure_is_not_reported_as_unchanged(self):
        text = release_summary({'version': {'outcome': 'success'},
                                'promote': {'outcome': 'failure'}}, '1.2.3', 'a'*40)
        self.assertIn('mögliche Teiländerungen', text)
        self.assertIn('einzelne Aliases aktualisiert', text)
        self.assertIn('v1.2.3', text)
        self.assertIn('a'*40, text)
        self.assertNotIn('Release abgeschlossen', text)

    def test_success_is_reported_only_after_publication(self):
        steps = {identifier: {'outcome': 'success'} for identifier, _ in STAGES}
        self.assertIn('Release abgeschlossen', release_summary(steps, '1.2.3', 'a'*40))
        steps['publish'] = {'outcome': 'cancelled'}
        self.assertNotIn('Release abgeschlossen', release_summary(steps, '1.2.3', 'a'*40))

    def test_missing_steps_are_not_confirmed(self):
        self.assertIn('nicht ausgeführt', release_summary({}, '1.2.3', 'a'*40))
