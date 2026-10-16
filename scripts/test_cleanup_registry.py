"""Protect GHCR releases, immutable commit evidence and manifest dependencies."""
import copy
from datetime import datetime, timedelta, timezone
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from cleanup_registry import ManifestMissing, assert_idle, cleanup, inventory, plan, references, registry_manifest

NOW = datetime.now(timezone.utc)
IMAGE = 'ghcr.io/example/viewer'
INDEX = 'application/vnd.oci.image.index.v1+json'
MANIFEST = 'application/vnd.oci.image.manifest.v1+json'


def digest(number):
    return 'sha256:' + f'{number:064x}'


def version(number, tags, age=30):
    date = (NOW - timedelta(days=age)).isoformat()
    return dict(id=number, name=digest(number), created_at=date, updated_at=date,
                metadata=dict(container=dict(tags=tags)))


def image(*children, subject=None):
    result = dict(schemaVersion=2, mediaType=INDEX if children else MANIFEST)
    if children:
        result['manifests'] = [dict(digest=digest(child)) for child in children]
    if subject is not None:
        result['subject'] = dict(digest=digest(subject))
    return result


class CleanupPlanTest(unittest.TestCase):
    def test_missing_external_subject_is_reported_without_blocking_cleanup(self):
        def fetch(_, key):
            if key == digest(99):
                raise ManifestMissing(key)
            return image(subject=99)
        result = plan([version(1, ['latest']), version(2, ['build-42-1-amd64'])],
                      IMAGE, fetch=fetch, now=NOW)
        self.assertEqual([v['id'] for v in result['candidates']], [2])
        self.assertEqual(result['missing_external_manifests'], [digest(99)])

    def test_missing_inventory_manifest_and_external_transport_error_abort(self):
        for error in (ManifestMissing('missing'), RuntimeError('unauthorized')):
            with self.subTest(error=error), self.assertRaises(RuntimeError):
                plan([version(1, ['latest'])], IMAGE,
                     fetch=lambda *_: (_ for _ in ()).throw(error))
        with self.assertRaisesRegex(RuntimeError, 'transport'):
            plan([version(1, ['latest'])], IMAGE,
                 fetch=lambda _, key: image(subject=99) if key == digest(1)
                 else (_ for _ in ()).throw(RuntimeError('transport')))

    def test_only_explicit_manifest_unknown_is_classified_as_missing(self):
        import subprocess
        for message in ('reading manifest: manifest unknown', 'unauthorized', 'timeout', 'name unknown'):
            with self.subTest(message=message), patch('cleanup_registry.subprocess.run',
                    return_value=subprocess.CompletedProcess([], 1, '', message)):
                with self.assertRaises(ManifestMissing if message.endswith('manifest unknown') else RuntimeError) as caught:
                    registry_manifest(IMAGE, digest(1))
                self.assertEqual(isinstance(caught.exception, ManifestMissing), message.endswith('manifest unknown'))

    def propose(self, versions, graph=None, **kwargs):
        graph = graph or {}
        return plan(versions, IMAGE, fetch=lambda _, key: graph.get(key, image()), now=NOW, **kwargs)

    def test_only_old_unreferenced_temporary_versions_are_candidates(self):
        versions = [version(1, ['latest', '1.2.3']), version(2, ['build-42-1-amd64']),
                    version(3, ['candidate-42-1']), version(4, ['build-43-1-arm64'], age=1),
                    version(5, []), version(6, ['sha-' + 'a'*40]), version(7, ['unrecognized'])]
        result = self.propose(versions)
        self.assertEqual([v['id'] for v in result['candidates']], [2, 3])
        self.assertEqual(result['retained'], 5)

    def test_release_commit_and_untagged_roots_protect_transitive_manifests(self):
        for root_tags in (['1.2.3'], ['latest'], ['sha-' + 'a'*40], []):
            with self.subTest(tags=root_tags):
                graph = {digest(1): image(2), digest(2): image(3), digest(3): image(subject=4)}
                versions = [version(1, root_tags), *[version(n, ['build-42-1-amd64']) for n in (2,3,4)]]
                self.assertEqual(self.propose(versions, graph)['candidates'], [])

    def test_mixed_tags_never_delete_the_whole_version(self):
        for tag in ['latest', '0.7', '0', '0.7.1', '0.8.0-beta.1', 'sha-'+'a'*40, 'custom']:
            self.assertEqual(self.propose([version(1, ['candidate-42-1', tag])])['candidates'], [])

    def test_an_old_digest_with_a_recent_tag_update_is_retained(self):
        item = version(1, ['build-42-1-amd64'])
        item['updated_at'] = NOW.isoformat()
        self.assertEqual(self.propose([item])['candidates'], [])

    def test_unknown_manifests_and_invalid_or_missing_descriptors_fail_closed(self):
        for bad in [{}, dict(schemaVersion=2, mediaType='unknown'),
                    dict(schemaVersion=2, mediaType=INDEX, manifests=[dict(digest='bad')])]:
            with self.assertRaises(ValueError):
                self.propose([version(1, ['latest']), version(2, ['build-42-1-amd64'])], {digest(1): bad})
        with self.assertRaises(RuntimeError):
            plan([version(1, ['latest'])], IMAGE, fetch=lambda *_: (_ for _ in ()).throw(RuntimeError('missing')))

    def test_duplicate_versions_and_unsafe_age_are_rejected(self):
        with self.assertRaises(ValueError):
            self.propose([version(1, []), version(1, [])])
        with self.assertRaises(ValueError):
            self.propose([], min_age_days=0)

    def test_cycles_and_attestation_annotations_are_traversed(self):
        manifest = image(2)
        manifest['annotations'] = {'vnd.docker.reference.digest':digest(3)}
        graph = {digest(1):manifest, digest(2):image(subject=1)}
        self.assertEqual(self.propose([version(1, ['latest']),version(2,['candidate-42-1']),version(3,['build-42-1-arm64'])], graph)['candidates'], [])

    def test_index_descriptor_attestation_reference_is_protected(self):
        manifest = image(2)
        manifest['manifests'][0]['annotations'] = {'vnd.docker.reference.digest':digest(3)}
        graph = {digest(1):manifest}
        self.assertEqual(self.propose([version(1,['latest']),version(2,[]),version(3,['build-42-1-arm64'])], graph)['candidates'], [])

    def test_subset_selection_cannot_delete_a_child_of_a_retained_candidate(self):
        versions = [version(1, ['candidate-42-1']), version(2, ['build-42-1-amd64'])]
        graph = {digest(1):image(2)}
        self.assertEqual([v['id'] for v in self.propose(versions, graph)['candidates']], [1,2])
        self.assertEqual(self.propose(versions, graph, selection={2})['candidates'], [])
        self.assertEqual([v['id'] for v in self.propose(versions, graph, selection={1})['candidates']], [1])


class CleanupExecutionTest(unittest.TestCase):
    def run_cleanup(self, selected=(), change_inventory=False, busy=False):
        self.versions = [version(1,['latest']),version(2,['build-42-1-amd64'])]
        current = copy.deepcopy(self.versions)
        if change_inventory:
            current[1]['metadata']['container']['tags'].append('sha-'+'a'*40)
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)/'plan.json'
            with patch('cleanup_registry.inventory', side_effect=[('endpoint', self.versions),('endpoint', current)]), \
                 patch('cleanup_registry.registry_manifest', return_value=image()), \
                 patch('cleanup_registry.assert_idle', side_effect=ValueError('busy') if busy else None), \
                 patch('cleanup_registry.api') as api:
                try:
                    cleanup('example/viewer', output, selected)
                except ValueError as error:
                    self.error = error
                else:
                    self.error = None
                self.proposal = json.loads(output.read_text())
                return api.call_args_list

    def test_default_preview_never_deletes(self):
        self.assertEqual(self.run_cleanup(), [])
        self.assertEqual([v['id'] for v in self.proposal['candidates']], [2])

    def test_only_explicit_revalidated_ids_are_deleted(self):
        calls = self.run_cleanup(selected=[2])
        self.assertIsNone(self.error)
        self.assertEqual(len(calls),1)
        self.assertEqual(calls[0].args, ('endpoint/2',))
        self.assertEqual(calls[0].kwargs, dict(method='DELETE'))

    def test_protected_unknown_changed_or_busy_state_prevents_deletion(self):
        for kwargs in [dict(selected=[1]),dict(selected=[99]),dict(selected=[2],change_inventory=True),dict(selected=[2],busy=True)]:
            with self.subTest(kwargs=kwargs):
                self.assertEqual(self.run_cleanup(**kwargs), [])
                self.assertIsNotNone(self.error)

    def test_active_main_or_release_jobs_block_cleanup(self):
        with patch('cleanup_registry.api', return_value=[dict(workflow_runs=[dict(id=42)])]):
            with self.assertRaisesRegex(ValueError,'läuft'):
                assert_idle('example/viewer')

    def test_paginated_inventory_supports_users_and_organizations(self):
        for owner_type, prefix in [('User','users'),('Organization','orgs')]:
            with patch('cleanup_registry.api', side_effect=[[dict(owner=dict(type=owner_type))], [[version(1,[])],[version(2,[])]]]) as api:
                endpoint, versions = inventory('example/viewer')
                self.assertEqual(endpoint,f'{prefix}/example/packages/container/viewer/versions')
                self.assertEqual(len(versions),2)
                self.assertIn('per_page=100',api.call_args_list[1].args[0])


if __name__ == '__main__':
    unittest.main()
