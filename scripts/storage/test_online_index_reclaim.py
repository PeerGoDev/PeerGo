import importlib.util
from pathlib import Path
import unittest

spec=importlib.util.spec_from_file_location('reclaim',Path(__file__).with_name('online_index_reclaim.py'))
reclaim=importlib.util.module_from_spec(spec)
spec.loader.exec_module(reclaim)

class PolicyTest(unittest.TestCase):
    def test_dead_pages_trigger_but_dense_live_index_does_not(self):
        self.assertTrue(reclaim.sparse(dict(leaf_pages=100,deleted_pages=900,empty_pages=0,avg_leaf_density=90)))
        self.assertFalse(reclaim.sparse(dict(leaf_pages=100,deleted_pages=0,empty_pages=0,avg_leaf_density=90)))
        self.assertFalse(reclaim.sparse(dict(leaf_pages=0,deleted_pages=0,empty_pages=0,avg_leaf_density=0)))

    def test_space_guard_preserves_headroom(self):
        gib=1024**3
        self.assertFalse(reclaim.enough_space(39*gib,1*gib))
        self.assertFalse(reclaim.enough_space(45*gib,16*gib))
        self.assertTrue(reclaim.enough_space(50*gib,16*gib))

    def test_accounting_and_raw_tables_are_not_unattended_targets(self):
        all_tables=sum(reclaim.TABLES.values(),[])
        for name in ['ledger.raw_session_intervals','ledger.seeding_evidence_items',
                     'ledger.seeding_evidence_sources','economy.seeding_reward_evidence_items',
                     'economy.magic_ledger_entries']:
            self.assertNotIn(name,all_tables)

if __name__=='__main__':unittest.main()
