# Compliance checklist

Where Taskwell stands on India's DPDP Act 2023 and Rules 2025, EU/UK GDPR, and SOC 2. Code items live in
`docs/compliance-plan.md` and `task.md`; this file tracks the documents and processes that code can't do.

## Before launch (legal, needs a lawyer)

- [ ] Fill every `[placeholder]` in `/privacy`, `/terms` and `/subprocessors`: legal name, address, grievance officer,
      VPS host and its country, SMTP provider, governing law.
- [ ] Lawyer review of the privacy notice, the terms, and a Data Processing Agreement (DPA). The DPA needs the GDPR
      Art. 28(3) terms, SCC modules 2/3 and the UK addendum, our security measures as an annex, a 14-day backup tail,
      and a sub-processor change notice.
- [ ] Sign the vendors' DPAs: Cloudflare, Anthropic, the VPS host and the SMTP provider.
- [ ] Decide whether an EU/UK representative is needed (GDPR Art. 27; likely yes when selling to the EU without an
      office there).
- [ ] Transfer impact assessment if the database host is outside the EU.
- [ ] Confirm the DPDP dates: notice, rights, breach and security rules apply from about 13 May 2027.

## Records of processing (GDPR Art. 30)

| Processing | Role | Data | People | Basis | Recipients | Kept |
|---|---|---|---|---|---|---|
| Accounts and sign-in | Controller | name, email, password hash, Google id, MFA secret (sealed) | users | contract / legitimate use | VPS host, SMTP | until deleted |
| Security logs | Controller | IP, browser, sign-in events, audit log | users | legitimate interest | VPS host | 1 year |
| Notification email | Controller | email, issue titles in mail | users | contract (opt-out in settings) | SMTP | not stored after sending |
| Billing | Paddle is controller | plan, Paddle customer id | workspace owners | contract / legal obligation | Paddle | while subscribed |
| Workspace content | Processor | issues, comments, chat, files, member names | customers' staff | customer's instructions | VPS host, Cloudflare R2, Anthropic (AI testing, opt-in) | until the customer deletes it, plus 14 days of backups |

DPIA: not needed. There is no special-category data, no monitoring of the public and no profiling with legal effect.
Member analytics is light workplace monitoring, covered by a short legitimate-interest note kept here.

## SOC 2 processes (not code)

- [ ] Policies: information security, access control (who can SSH or open the database, quarterly access review),
      change management, incident response (see `runbooks.md`), business continuity and disaster recovery (RTO/RPO,
      monthly restore drill), retention and deletion, encryption and key management (env-file key rotation), vendor
      management, and risk assessment.
- [ ] GitHub: branch protection on `master` (PR required, CI green, one review), CODEOWNERS, Dependabot. Fix the CI
      `images` job: `deploy/backend.Dockerfile` and `deploy/web.Dockerfile` are missing from git.
- [ ] Production: set `ADMIN_ALLOWED_IPS`, ship logs off-box with a year of retention, add an external uptime check
      on `https://api.taskwell.dev/healthz`, alert on backup failure, confirm disk encryption with the host, and set
      `RCLONE_REMOTE` (`deploy/README.md`).
- [ ] People: onboarding and offboarding checklist, security training, background checks.
- [ ] Evidence: vendor list with their SOC 2 reports, asset inventory, a yearly pentest, quarterly vulnerability scan
      records, and the restore drill log.
- [ ] Replace `security.md` (a pentest tool's output against localhost) with a vulnerability disclosure policy:
      security@taskwell.dev, what's in scope, safe harbour. Re-test its webhook SSRF and stored XSS findings against
      production first. `netguard` and the document sanitizer should already cover them.

## Later product controls (not built, in priority order)

1. Workspace setting "Require two-factor sign-in".
2. Re-authentication before creating API tokens and before deleting a workspace.
3. Seal the secret `system_config` values (SMTP password, S3 secret, Google secret, Paddle keys) with `secret.Seal`.
4. Idle timeout on refresh tokens.
5. Breached-password check (HIBP k-anonymity, fails open).
6. Inactive-account purge with a warning email 48 hours ahead (DPDP Rule 8; optional for Taskwell).
