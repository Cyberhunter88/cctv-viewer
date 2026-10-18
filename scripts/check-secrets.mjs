#!/usr/bin/env node
import { execFileSync, spawnSync } from 'node:child_process';
import { readFileSync } from 'node:fs';

const git = (...args) => execFileSync('git', args, { encoding: 'utf8', maxBuffer: 64 * 1024 * 1024 }).trimEnd();
const zero = /^0+$/;
const mode = process.argv[2];
if (!['--pre-push', '--history', '--working-tree'].includes(mode)) {
  console.error('Usage: node scripts/check-secrets.mjs --pre-push|--history|--working-tree');
  process.exit(2);
}

const findings = [];
const sensitivePath = /(^|\/)(\.env(?:\..+)?|id_(?:rsa|ed25519)|[^/]+\.(?:pem|p12|pfx|key)|credentials(?:\.[^/]*)?|secrets?\/[^/]+)$/i;
const generatedPath = /(^|\/)(node_modules|\.local-reports|test-results|playwright-report|coverage|data|bin)\//i;
const placeholder = /^(?:<[^>]+>|\$\{[^}]+\}|\$[A-Z_]+|example|placeholder|dummy|changeme|your[_-].*|(?:x|\*){4,})$/i;
const rules = [
  ['private_key', /-----BEGIN (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----/g, 'HIGH'],
  ['github_token', /\b(?:gh[pousr]_[A-Za-z0-9_]{20,}|github_pat_[A-Za-z0-9_]{20,})\b/g, 'HIGH'],
  ['provider_token', /\b(?:AKIA[0-9A-Z]{16}|sk_live_[A-Za-z0-9]{16,}|xox[baprs]-[A-Za-z0-9-]{20,})\b/g, 'HIGH'],
  ['url_credentials', /\b[a-z][a-z0-9+.-]*:\/\/[^\s/:@]+:[^\s/@]+@[^\s/]+/gi, 'HIGH'],
  ['credential_assignment', /\b(?:password|passwd|secret|token|api[_-]?key|client[_-]?secret)\b\s*[:=]\s*["']([^"'\r\n]{8,})["']/gi, 'SUSPECTED'],
];
function mask(value) { return value.length <= 4 ? '****' : `${value.slice(0, 2)}…${value.slice(-2)}`; }
function scan(path, blob, commit) {
  if (generatedPath.test(path)) findings.push({ path, commit, line: '-', rule: 'generated_path', risk: 'SUSPECTED', value: '[path]' });
  if (sensitivePath.test(path) && path !== '.env.example') findings.push({ path, commit, line: '-', rule: 'sensitive_path', risk: 'HIGH', value: '[path]' });
  if (blob.length > 25 * 1024 * 1024) { findings.push({ path, commit, line: '-', rule: 'large_object', risk: 'SUSPECTED', value: '[size]' }); return; }
  if (blob.includes(0)) { if (!/\.(?:ico|png|jpe?g|gif|webp|woff2?)$/i.test(path)) findings.push({ path, commit, line: '-', rule: 'binary_object', risk: 'SUSPECTED', value: '[binary]' }); return; }
  for (const [index, line] of blob.toString('utf8').split('\n').entries()) {
    for (const [rule, pattern, risk] of rules) {
      pattern.lastIndex = 0;
      for (const match of line.matchAll(pattern)) {
        const value = rule === 'credential_assignment' ? match[1] : match[0];
        if (placeholder.test(value) || /example\.(?:org|com|net)|synthetic|fixture|test[-_ ]?only/i.test(value)) continue;
        findings.push({ path, commit, line: index + 1, rule, risk, value: mask(value) });
      }
    }
  }
}
function scanCommits(commits) {
  const seen = new Set();
  for (const commit of commits) {
    for (const row of git('ls-tree', '-r', '--full-tree', commit).split('\n').filter(Boolean)) {
      const match = row.match(/^\d+ blob ([0-9a-f]+)\t(.+)$/);
      if (!match) continue;
      const [, blobId, path] = match;
      if (seen.has(`${blobId}\0${path}`)) continue;
      seen.add(`${blobId}\0${path}`);
      const result = spawnSync('git', ['cat-file', 'blob', blobId], { maxBuffer: 26 * 1024 * 1024 });
      if (result.error || result.status !== 0) { findings.push({ path, commit, line: '-', rule: 'unreadable_object', risk: 'HIGH', value: '[object]' }); continue; }
      scan(path, result.stdout, commit.slice(0, 12));
    }
  }
}
if (mode === '--working-tree') {
  for (const path of git('ls-files').split('\n').filter(Boolean)) scan(path, readFileSync(path), 'working-tree');
} else if (mode === '--history') {
  scanCommits(git('rev-list', '--all').split('\n').filter(Boolean));
} else {
  const updates = readFileSync(0, 'utf8').split('\n').filter(Boolean);
  const commits = new Set();
  for (const update of updates) {
    const [, localSha, , remoteSha] = update.trim().split(/\s+/);
    if (!localSha || zero.test(localSha)) continue;
    const args = ['rev-list', localSha];
    if (remoteSha && !zero.test(remoteSha)) args.push(`^${remoteSha}`);
    else args.push('--not', '--remotes');
    for (const commit of git(...args).split('\n').filter(Boolean)) commits.add(commit);
  }
  scanCommits(commits);
}
for (const item of findings) console.error(`${item.risk} ${item.path}:${item.line} commit=${item.commit} rule=${item.rule} value=${item.value}`);
console.error(`Secret scan: ${findings.length} finding(s).`);
if (findings.length) process.exit(1);
