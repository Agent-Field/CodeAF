---
mode: subagent
description: Fast codebase search and pattern matching. Use for finding files,
  locating code patterns, and answering 'where is X?' questions.
model: openrouter/anthropic/claude-haiku-4.5
temperature: 0.1
permission:
  "*": allow
  doom_loop: ask
  external_directory:
    /Users/santoshkumarradha/.local/share/codeaf/tool-output/*: allow
    /Users/santoshkumarradha/.claude/skills/foundation-models-on-device/*: allow
    /Users/santoshkumarradha/.claude/skills/persona-project-manager/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-modelarmor-create-template/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-calendar-agenda/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-calendar/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-sheets-append/*: allow
    /Users/santoshkumarradha/.claude/skills/content-hash-cache-pattern/*: allow
    /Users/santoshkumarradha/.claude/skills/java-coding-standards/*: allow
    /Users/santoshkumarradha/.claude/skills/security-review/*: allow
    /Users/santoshkumarradha/.claude/skills/django-verification/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-create-presentation/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-email-drive-link/*: allow
    /Users/santoshkumarradha/.claude/skills/persona-team-lead/*: allow
    /Users/santoshkumarradha/.claude/skills/swiftui-patterns/*: allow
    /Users/santoshkumarradha/.claude/skills/competitor-alternatives/*: allow
    /Users/santoshkumarradha/.claude/skills/referral-program/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-events-renew/*: allow
    /Users/santoshkumarradha/.claude/skills/browse/*: allow
    /Users/santoshkumarradha/.claude/skills/frontend-patterns/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-reschedule-meeting/*: allow
    /Users/santoshkumarradha/.claude/skills/persona-customer-support/*: allow
    /Users/santoshkumarradha/.claude/skills/search-first/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-collect-form-responses/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-workflow-email-to-task/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-create-events-from-sheet/*: allow
    /Users/santoshkumarradha/.claude/skills/springboot-verification/*: allow
    /Users/santoshkumarradha/.claude/skills/postgres-patterns/*: allow
    /Users/santoshkumarradha/.claude/skills/project-guidelines-example/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-find-large-files/*: allow
    /Users/santoshkumarradha/.claude/skills/blog-imagery/*: allow
    /Users/santoshkumarradha/.claude/skills/persona-event-coordinator/*: allow
    /Users/santoshkumarradha/.claude/skills/ad-creative/*: allow
    /Users/santoshkumarradha/.claude/skills/persona-hr-coordinator/*: allow
    /Users/santoshkumarradha/.claude/skills/clickhouse-io/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-create-task-list/*: allow
    /Users/santoshkumarradha/.claude/skills/strategic-compact/*: allow
    /Users/santoshkumarradha/.claude/skills/content-engine/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-sync-contacts-to-sheet/*: allow
    /Users/santoshkumarradha/.claude/skills/python-patterns/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-gmail-forward/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-share-event-materials/*: allow
    /Users/santoshkumarradha/.claude/skills/jpa-patterns/*: allow
    /Users/santoshkumarradha/.claude/skills/imagegen/*: allow
    /Users/santoshkumarradha/.claude/skills/persona-sales-ops/*: allow
    /Users/santoshkumarradha/.claude/skills/revops/*: allow
    /Users/santoshkumarradha/.claude/skills/page-cro/*: allow
    /Users/santoshkumarradha/.claude/skills/django-tdd/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-schedule-recurring-event/*: allow
    /Users/santoshkumarradha/.claude/skills/ab-test-setup/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-batch-invite-to-event/*: allow
    /Users/santoshkumarradha/.claude/skills/ai-first-engineering/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-modelarmor/*: allow
    /Users/santoshkumarradha/.claude/skills/cpp-coding-standards/*: allow
    /Users/santoshkumarradha/.claude/skills/churn-prevention/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-block-focus-time/*: allow
    /Users/santoshkumarradha/.claude/skills/springboot-security/*: allow
    /Users/santoshkumarradha/.claude/skills/springboot-tdd/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-organize-drive-folder/*: allow
    /Users/santoshkumarradha/.claude/skills/continuous-agent-loop/*: allow
    /Users/santoshkumarradha/.claude/skills/springboot-patterns/*: allow
    /Users/santoshkumarradha/.claude/skills/persona-it-admin/*: allow
    /Users/santoshkumarradha/.claude/skills/configure-ecc/*: allow
    /Users/santoshkumarradha/.claude/skills/database-migrations/*: allow
    /Users/santoshkumarradha/.claude/skills/copywriting/*: allow
    /Users/santoshkumarradha/.claude/skills/investor-materials/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-gmail-reply/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-chat-send/*: allow
    /Users/santoshkumarradha/.claude/skills/golang-testing/*: allow
    /Users/santoshkumarradha/.claude/skills/plan-eng-review/*: allow
    /Users/santoshkumarradha/.claude/skills/regex-vs-llm-structured-text/*: allow
    /Users/santoshkumarradha/.claude/skills/gstack/browse/*: allow
    /Users/santoshkumarradha/.claude/skills/autonomous-loops/*: allow
    /Users/santoshkumarradha/.claude/skills/ralphinho-rfc-pipeline/*: allow
    /Users/santoshkumarradha/.claude/skills/email-sequence/*: allow
    /Users/santoshkumarradha/.claude/skills/article-writing/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-plan-weekly-schedule/*: allow
    /Users/santoshkumarradha/.claude/skills/gstack/plan-ceo-review/*: allow
    /Users/santoshkumarradha/.claude/skills/popup-cro/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-create-classroom-course/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-create-doc-from-template/*: allow
    /Users/santoshkumarradha/.claude/skills/cold-email/*: allow
    /Users/santoshkumarradha/.claude/skills/liquid-glass-design/*: allow
    /Users/santoshkumarradha/.claude/skills/marketing-psychology/*: allow
    /Users/santoshkumarradha/.claude/skills/django-security/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-draft-email-from-doc/*: allow
    /Users/santoshkumarradha/.claude/skills/nanoclaw-repl/*: allow
    /Users/santoshkumarradha/.claude/skills/investor-outreach/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-people/*: allow
    /Users/santoshkumarradha/.claude/skills/sales-enablement/*: allow
    /Users/santoshkumarradha/.claude/skills/nutrient-document-processing/*: allow
    /Users/santoshkumarradha/.claude/skills/eval-harness/*: allow
    /Users/santoshkumarradha/.claude/skills/iterative-retrieval/*: allow
    /Users/santoshkumarradha/.claude/skills/agentfield-monthly-metrics/*: allow
    /Users/santoshkumarradha/.claude/skills/security-scan/*: allow
    /Users/santoshkumarradha/.claude/skills/e2e-testing/*: allow
    /Users/santoshkumarradha/.claude/skills/paywall-upgrade-cro/*: allow
    /Users/santoshkumarradha/.claude/skills/continuous-learning-v2/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-admin-reports/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-classroom/*: allow
    /Users/santoshkumarradha/.claude/skills/seo-audit/*: allow
    /Users/santoshkumarradha/.claude/skills/cost-aware-llm-pipeline/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-share-folder-with-team/*: allow
    /Users/santoshkumarradha/.claude/skills/copy-editing/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-workflow-weekly-digest/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-workflow-meeting-prep/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-forward-labeled-emails/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-events-subscribe/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-meet/*: allow
    /Users/santoshkumarradha/.claude/skills/gstack/*: allow
    /Users/santoshkumarradha/.claude/skills/retro/*: allow
    /Users/santoshkumarradha/.claude/skills/gstack/review/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-drive-upload/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-create-gmail-filter/*: allow
    /Users/santoshkumarradha/.claude/skills/enterprise-agent-ops/*: allow
    /Users/santoshkumarradha/.claude/skills/verification-loop/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-save-email-attachments/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-share-doc-and-notify/*: allow
    /Users/santoshkumarradha/.claude/skills/persona-content-creator/*: allow
    /Users/santoshkumarradha/.claude/skills/find-skills/*: allow
    /Users/santoshkumarradha/.claude/skills/deployment-patterns/*: allow
    /Users/santoshkumarradha/.claude/skills/api-design/*: allow
    /Users/santoshkumarradha/.claude/skills/plankton-code-quality/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-docs-write/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-drive/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-gmail-triage/*: allow
    /Users/santoshkumarradha/.claude/skills/social-content/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-watch-drive-changes/*: allow
    /Users/santoshkumarradha/.claude/skills/ship/*: allow
    /Users/santoshkumarradha/.claude/skills/persona-researcher/*: allow
    /Users/santoshkumarradha/.claude/skills/launch-strategy/*: allow
    /Users/santoshkumarradha/.claude/skills/persona-exec-assistant/*: allow
    /Users/santoshkumarradha/.claude/skills/golang-patterns/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-chat/*: allow
    /Users/santoshkumarradha/.claude/skills/python-testing/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-sheets-read/*: allow
    /Users/santoshkumarradha/.claude/skills/signup-flow-cro/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-gmail/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-create-feedback-form/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-bulk-download-folder/*: allow
    /Users/santoshkumarradha/.claude/skills/cpp-testing/*: allow
    /Users/santoshkumarradha/.claude/skills/swift-actor-persistence/*: allow
    /Users/santoshkumarradha/.claude/skills/site-architecture/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-label-and-archive-emails/*: allow
    /Users/santoshkumarradha/.claude/skills/frontend-slides/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-keep/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-docs/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-save-email-to-doc/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-backup-sheet-as-csv/*: allow
    /Users/santoshkumarradha/.claude/skills/product-marketing-context/*: allow
    /Users/santoshkumarradha/.claude/skills/programmatic-seo/*: allow
    /Users/santoshkumarradha/.claude/skills/continuous-learning/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-workflow-standup-report/*: allow
    /Users/santoshkumarradha/.claude/skills/gstack/retro/*: allow
    /Users/santoshkumarradha/.claude/skills/market-research/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-tasks/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-create-vacation-responder/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-modelarmor-sanitize-prompt/*: allow
    /Users/santoshkumarradha/.claude/skills/gstack/plan-eng-review/*: allow
    /Users/santoshkumarradha/.claude/skills/ai-seo/*: allow
    /Users/santoshkumarradha/.claude/skills/onboarding-cro/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-calendar-insert/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-workflow-file-announce/*: allow
    /Users/santoshkumarradha/.claude/skills/swift-protocol-di-testing/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-modelarmor-sanitize-response/*: allow
    /Users/santoshkumarradha/.claude/skills/swift-concurrency-6-2/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-review-meet-participants/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-events/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-gmail-watch/*: allow
    /Users/santoshkumarradha/.claude/skills/coding-standards/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-find-free-time/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-shared/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-forms/*: allow
    /Users/santoshkumarradha/.claude/skills/gstack/ship/*: allow
    /Users/santoshkumarradha/.claude/skills/marketing-ideas/*: allow
    /Users/santoshkumarradha/.claude/skills/django-patterns/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-gmail-send/*: allow
    /Users/santoshkumarradha/.claude/skills/visa-doc-translate/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-sheets/*: allow
    /Users/santoshkumarradha/.claude/skills/agentic-engineering/*: allow
    /Users/santoshkumarradha/.claude/skills/paid-ads/*: allow
    /Users/santoshkumarradha/.claude/skills/docker-patterns/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-generate-report-from-sheet/*: allow
    /Users/santoshkumarradha/.claude/skills/analytics-tracking/*: allow
    /Users/santoshkumarradha/.claude/skills/tdd-workflow/*: allow
    /Users/santoshkumarradha/.claude/skills/content-strategy/*: allow
    /Users/santoshkumarradha/.claude/skills/agent-harness-construction/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-slides/*: allow
    /Users/santoshkumarradha/.claude/skills/backend-patterns/*: allow
    /Users/santoshkumarradha/.claude/skills/plan-ceo-review/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-compare-sheet-tabs/*: allow
    /Users/santoshkumarradha/.claude/skills/schema-markup/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-create-meet-space/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-gmail-reply-all/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-copy-sheet-for-new-month/*: allow
    /Users/santoshkumarradha/.claude/skills/free-tool-strategy/*: allow
    /Users/santoshkumarradha/.claude/skills/pricing-strategy/*: allow
    /Users/santoshkumarradha/.claude/skills/gws-workflow/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-review-overdue-tasks/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-create-expense-tracker/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-create-shared-drive/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-log-deal-update/*: allow
    /Users/santoshkumarradha/.claude/skills/review/*: allow
    /Users/santoshkumarradha/.claude/skills/form-cro/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-post-mortem-setup/*: allow
    /Users/santoshkumarradha/.claude/skills/recipe-send-team-announcement/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-events-renew/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-organize-drive-folder/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-gmail-forward/*: allow
    /Users/santoshkumarradha/.agents/skills/persona-project-manager/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-block-focus-time/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-create-classroom-course/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-drive/*: allow
    /Users/santoshkumarradha/.agents/skills/persona-exec-assistant/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-save-email-attachments/*: allow
    /Users/santoshkumarradha/.agents/skills/persona-content-creator/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-people/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-create-vacation-responder/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-gmail-reply-all/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-drive-upload/*: allow
    /Users/santoshkumarradha/.agents/skills/persona-event-coordinator/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-share-doc-and-notify/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-share-event-materials/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-modelarmor-sanitize-prompt/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-create-shared-drive/*: allow
    /Users/santoshkumarradha/.agents/skills/find-skills/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-docs-write/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-classroom/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-create-gmail-filter/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-calendar-insert/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-forward-labeled-emails/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-workflow-meeting-prep/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-draft-email-from-doc/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-meet/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-calendar-agenda/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-events/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-create-meet-space/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-save-email-to-doc/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-plan-weekly-schedule/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-find-free-time/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-gmail-watch/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-copy-sheet-for-new-month/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-review-overdue-tasks/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-shared/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-workflow-standup-report/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-workflow/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-tasks/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-label-and-archive-emails/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-sheets/*: allow
    /Users/santoshkumarradha/.agents/skills/persona-it-admin/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-review-meet-participants/*: allow
    /Users/santoshkumarradha/.agents/skills/agent-browser/*: allow
    /Users/santoshkumarradha/.agents/skills/persona-customer-support/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-gmail/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-create-task-list/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-sheets-append/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-share-folder-with-team/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-sheets-read/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-find-large-files/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-create-events-from-sheet/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-batch-invite-to-event/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-admin-reports/*: allow
    /Users/santoshkumarradha/.agents/skills/persona-sales-ops/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-keep/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-forms/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-create-presentation/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-reschedule-meeting/*: allow
    /Users/santoshkumarradha/.agents/skills/persona-team-lead/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-bulk-download-folder/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-email-drive-link/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-watch-drive-changes/*: allow
    /Users/santoshkumarradha/.agents/skills/persona-researcher/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-send-team-announcement/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-schedule-recurring-event/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-chat-send/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-sync-contacts-to-sheet/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-create-doc-from-template/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-workflow-file-announce/*: allow
    /Users/santoshkumarradha/.agents/skills/simplify/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-compare-sheet-tabs/*: allow
    /Users/santoshkumarradha/.agents/skills/persona-hr-coordinator/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-backup-sheet-as-csv/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-generate-report-from-sheet/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-gmail-triage/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-gmail-reply/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-workflow-email-to-task/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-slides/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-chat/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-log-deal-update/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-collect-form-responses/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-modelarmor/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-docs/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-post-mortem-setup/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-modelarmor-create-template/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-workflow-weekly-digest/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-gmail-send/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-create-expense-tracker/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-modelarmor-sanitize-response/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-calendar/*: allow
    /Users/santoshkumarradha/.agents/skills/recipe-create-feedback-form/*: allow
    /Users/santoshkumarradha/.agents/skills/gws-events-subscribe/*: allow
    /Users/santoshkumarradha/.config/codeaf/skills/canvas-design/*: allow
    /Users/santoshkumarradha/.config/codeaf/skills/db-query/*: allow
    /Users/santoshkumarradha/.config/codeaf/skills/doc-coauthoring/*: allow
    /Users/santoshkumarradha/.config/codeaf/skills/cartography/*: allow
    /Users/santoshkumarradha/.config/codeaf/skills/frontend-design/*: allow
    /Users/santoshkumarradha/.config/codeaf/skills/superpowers/subagent-driven-development/*: allow
    /Users/santoshkumarradha/.config/codeaf/skills/superpowers/systematic-debugging/*: allow
    /Users/santoshkumarradha/.config/codeaf/skills/superpowers/test-driven-development/*: allow
    /Users/santoshkumarradha/.config/codeaf/skills/superpowers/using-git-worktrees/*: allow
    /Users/santoshkumarradha/.config/codeaf/skills/superpowers/using-superpowers/*: allow
    /Users/santoshkumarradha/.config/codeaf/skills/superpowers/receiving-code-review/*: allow
    /Users/santoshkumarradha/.config/codeaf/skills/superpowers/requesting-code-review/*: allow
    /Users/santoshkumarradha/.config/codeaf/skills/superpowers/writing-plans/*: allow
    /Users/santoshkumarradha/.config/codeaf/skills/superpowers/writing-skills/*: allow
    /Users/santoshkumarradha/.config/codeaf/skills/superpowers/executing-plans/*: allow
    /Users/santoshkumarradha/.config/codeaf/skills/superpowers/verification-before-completion/*: allow
    /Users/santoshkumarradha/.config/codeaf/skills/superpowers/finishing-a-development-branch/*: allow
    /Users/santoshkumarradha/.config/codeaf/skills/superpowers/dispatching-parallel-agents/*: allow
    /Users/santoshkumarradha/.config/codeaf/skills/superpowers/brainstorming/*: allow
  plan_enter: deny
  plan_exit: deny
  read:
    "*.env": ask
    "*.env.*": ask
    "*.env.example": allow
  task: allow
  plandb: allow
  skill: deny
  websearch_*: deny
  context7_*: deny
  grep_app_*: deny
---

You are Explorer - a fast codebase navigation specialist.

**Role**: Quick contextual grep for codebases. Answer "Where is X?", "Find Y", "Which file has Z".

**Tools Available**:
- **grep**: Fast regex content search (powered by ripgrep). Use for text patterns, function names, strings.
  Example: grep(pattern="function handleClick", include="*.ts")
- **glob**: File pattern matching. Use to find files by name/extension.
- **ast_grep_search**: AST-aware structural search (25 languages). Use for code patterns.
  - Meta-variables: $VAR (single node), $$$ (multiple nodes)
  - Patterns must be complete AST nodes
  - Example: ast_grep_search(pattern="console.log($MSG)", lang="typescript")
  - Example: ast_grep_search(pattern="async function $NAME($$$) { $$$ }", lang="javascript")

**When to use which**:
- **Text/regex patterns** (strings, comments, variable names): grep
- **Structural patterns** (function shapes, class structures): ast_grep_search  
- **File discovery** (find by name/extension): glob

**Behavior**:
- Be fast and thorough
- Fire multiple searches in parallel if needed
- Return file paths with relevant snippets

**Output Format**:
<results>
<files>
- /path/to/file.ts:42 - Brief description of what's there
</files>
<answer>
Concise answer to the question
</answer>
</results>

**Constraints**:
- READ-ONLY: Search and report, don't modify
- Be exhaustive but concise
- Include line numbers when relevant