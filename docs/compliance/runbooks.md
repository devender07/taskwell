# Runbooks

Draft for the team; have a lawyer confirm the legal deadlines before relying on them.

## Personal data breach

A breach is any unauthorised access to, loss of, or change to personal data: a leaked database dump or backup, a
cross-workspace bug that showed one customer's data to another, a stolen admin session, a misdirected email with data.

1. **Contain (hour 0).** Stop the leak: revoke the credential, take the endpoint down, rotate the keys in
   `/etc/taskwell/*.env` (`MFA_ENCRYPTION_KEY` rotation makes sealed secrets unreadable; reseal first). Keep the
   evidence: copy the relevant journald logs, `workspace_audit`, `admin_audit` and `account_events` rows before the
   sweeper ages anything out.
2. **Decide (within 24 h).** Who owns the incident (default: the founder). Write in the breach register (below) what
   happened, when it started and was found, what data and whose, how many people, and the likely harm.
3. **Notify.**
   - **India, DPDP Rule 7:** every breach, whatever the size. Tell the Data Protection Board without delay, and send
     the full report (facts, cause, mitigation, people affected, notices sent) within **72 hours**. Tell the affected
     people without delay: what happened, when, the likely impact, what we did, what they should do, and our contact.
   - **EU/UK, GDPR Art. 33/34:** tell the supervisory authority within **72 hours** unless the breach is unlikely to
     put people at risk. Tell the people themselves without undue delay when the risk is high.
   - **Customers (as processor):** email each affected workspace's admins without undue delay. They are the
     controllers of their workspace content and have their own notification duties.
   - Send the emails with `mail.Render` (a heading, what happened, what to do, contact) through the normal queue.
4. **Fix and review (within 2 weeks).** Root cause, the fix, a test that would have caught it, and the register entry
   closed with lessons learned.

### Breach register

Keep this outside the product (a private doc). Record every incident, including ones not reported:

| Date found | Started | What happened | Data / people | Reported to (when) | Fix | Closed |
|---|---|---|---|---|---|---|

## Restore drill (monthly)

Proves the backups work (SOC 2 A1.3). On the server:

```sh
F=$(sudo /opt/taskwell-db/backup.sh list | sort -k2,3 | tail -1 | awk '{print $NF}')   # the newest in R2
sudo /opt/taskwell-db/backup.sh get "$F" /tmp && F=/tmp/$F
docker exec taskwell-postgres createdb -U taskwell restore_check
docker exec -i taskwell-postgres pg_restore -U taskwell -d restore_check --no-owner < "$F"
docker exec taskwell-postgres psql -U taskwell -d restore_check -Atc "select count(*) from users; select count(*) from issues"
docker exec taskwell-postgres dropdb -U taskwell restore_check && sudo rm "$F"
```

Log the date, the dump
used, the counts and how long it took in the drill log. That log is the evidence an auditor asks for.

## Data subject requests

Most are self-serve: Settings → Profile has download, delete and correction. Handle emails to privacy@taskwell.dev:

- **Within 30 days**, the window the privacy notice promises (DPDP allows up to 90, GDPR one month).
- Verify identity by replying from the account's email address.
- **About workspace content** (issues, comments, files): send the person to the workspace's admins, who control it,
  and help the admins if they ask.
- **Nomination (DPDP s.14):** record who was named. On proof of death or incapacity, act for them as the account
  owner.
- Note each request and its answer in a private log.
