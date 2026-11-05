// The documentation site's one configuration: what it is called, where the
// canonical source lives and how the pages are ordered. Pages live in
// src/pages/<slug>.html; the navigation below is the only place their order is
// decided.

export default {
  title: 'Archon',
  tagline: 'Chain coding agents and gates.',
  // Links written as repo:<path>[#anchor] resolve here and are checked against
  // this checkout at build time.
  repoUrl: 'https://github.com/Perttulands/Archon-agentgraphs',
  repoBranch: 'main',
  // The newest public release. The pages describe the current source, which
  // is newer; versions.html says what differs.
  release: {
    tag: 'v0.1.0',
    date: '2026-09-14',
    url: 'https://github.com/Perttulands/Archon-agentgraphs/releases/tag/v0.1.0',
  },
  nav: [
    { section: 'Start', pages: ['index', 'concepts', 'versions'] },
    { section: 'Get started', pages: ['install', 'quickstart', 'first-real-run'] },
    { section: 'Build missions', pages: ['missions', 'formations', 'gates', 'limits'] },
    { section: 'Run and drive', pages: ['runs', 'pi', 'agents-drive'] },
    { section: 'Operate', pages: ['operate', 'recovery'] },
    { section: 'Reference', pages: ['cli', 'mission-file', 'http-api', 'codes'] },
    { section: 'Project', pages: ['examples', 'contributing'] },
  ],
}
