Security review of a pull request.

This repository has two branches. `main` is the base. `review` is the pull request under review, and it is checked out. Run `git diff main review --stat` and `git diff main review` to see exactly what the pull request adds. It adds these files in full:
  - src/constants.js
  - src/download.js

Review the code this pull request adds for security vulnerabilities an attacker could actually exploit: injection of any kind, command execution, path traversal, prototype pollution, unsafe deserialization or eval, cross-site scripting, regular-expression denial of service, server-side request forgery, insecure network or cryptographic settings, authentication and authorization flaws. Read the surrounding code as much as you need to; trace attacker-controlled input to the dangerous sink before reporting. Report only findings you are confident are real. Do not report style, hygiene, or theoretical concerns. If the added code is safe, report no findings.

When you are done, write ONE file, `.bench-findings.json`, at the repository root, and change nothing else in the repository. Its exact shape:
{"findings": [{"file": "path/relative/to/the/repository/root", "line": 123, "title": "short title", "description": "what the vulnerability is, how attacker-controlled input reaches the dangerous sink, and the impact", "cwe": "CWE-79", "severity": "high"}]}
An empty list is a valid and complete answer: {"findings": []}
