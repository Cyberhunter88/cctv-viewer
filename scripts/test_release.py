import unittest
from release import tags_for

class ReleaseTest(unittest.TestCase):
    def test_first_stable(self):
        self.assertEqual(tags_for('v1.2.3',False,[]),['1.2.3','latest','1','1.2'])
    def test_backport(self):
        releases=[{'tag_name':'v2.0.0'},{'tag_name':'v1.3.0'}]
        self.assertEqual(tags_for('v1.2.4',False,releases),['1.2.4','1.2'])
    def test_old_patch(self):
        self.assertEqual(tags_for('v1.2.3',False,[{'tag_name':'v1.2.4'}]),['1.2.3'])
    def test_prerelease(self):
        self.assertEqual(tags_for('v1.0.0-beta.1',True,[]),['1.0.0-beta.1'])
    def test_bad_tags(self):
        for tag,pre in [('foo',False),('v01.0.0',False),('v1.0.0',True),('v1.0.0-rc.01',True),('v1.0.0-beta.1',False),('v1.0.0;echo bad',False)]:
            with self.assertRaises(ValueError): tags_for(tag,pre,[])
    def test_drafts_ignored(self):
        self.assertIn('latest',tags_for('v1.0.0',False,[{'tag_name':'v9.0.0','draft':True}]))

if __name__=='__main__': unittest.main()
