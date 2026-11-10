import { Link, createFileRoute } from '@tanstack/react-router'
import { LegalPage } from '@/components/legal/legal-page'

export const Route = createFileRoute('/privacy')({
  component: Privacy,
})

const mail = <a href="mailto:privacy@taskwell.dev">privacy@taskwell.dev</a>

function Privacy() {
  return (
    <LegalPage title="Privacy notice">
      <p>
        This notice explains what personal data Taskwell collects, why, how long we keep it and the rights you have. It covers India's Digital
        Personal Data Protection Act 2023 and Rules 2025 (DPDP) and the EU and UK General Data Protection Regulation (GDPR).
      </p>

      <h2>Who we are</h2>
      <p>
        Taskwell is run by [Company legal name], [Registered address, India]. For your account data we are the controller (the "data fiduciary"
        under DPDP). For the content of a workspace (issues, comments, chat, files) we are a processor: that content belongs to the workspace, and
        we handle it on the instructions of the workspace's admins.
      </p>
      <p>
        Contact and grievance officer: [Name], {mail}. We reply within 30 days. EU and UK representative: [to appoint].
      </p>

      <h2>What we collect and why</h2>
      <ul>
        <li>
          <strong>Account:</strong> your name, email, password hash and, if you sign in with Google, your Google account id. We use it to provide the
          service you signed up for (contract; legitimate use under DPDP).
        </li>
        <li>
          <strong>Security logs:</strong> IP address, browser, sign-ins and account changes, and the workspace audit log. We use them to keep accounts
          and workspaces secure (legitimate interest), and keep them for 1 year.
        </li>
        <li>
          <strong>Billing:</strong> payments go through Paddle, our merchant of record. Paddle is its own controller for payment data; we only keep a
          link to your Paddle customer and subscription.
        </li>
        <li>
          <strong>Emails we send:</strong> sign-in codes, security notices and notifications. You can turn notifications off in settings.
        </li>
      </ul>
      <p>We don't send marketing email, show ads, sell your data or use tracking cookies.</p>

      <h2>Cookies and local storage</h2>
      <p>
        We only use strictly necessary cookies: your sign-in session, its refresh, the two-factor step, the sign-up check and the Google sign-in
        state. Your browser's local storage keeps your theme and interface preferences. Our analytics, Cloudflare Web Analytics, uses no cookies.
        Because of this there is no cookie banner.
      </p>

      <h2>How long we keep it</h2>
      <ul>
        <li>Your account: until you delete it.</li>
        <li>Security log and workspace audit log: 1 year.</li>
        <li>Trash: 30 days.</li>
        <li>Notifications: 90 days once read, otherwise 1 year.</li>
        <li>Webhook deliveries: 30 days.</li>
        <li>Backups: 14 days, so deleted data can stay in backups for up to 14 days.</li>
      </ul>

      <h2>Your rights</h2>
      <ul>
        <li>
          <strong>Access and download:</strong> download your data in Settings → Profile.
        </li>
        <li>
          <strong>Correction:</strong> edit your name and change your email in Settings → Profile.
        </li>
        <li>
          <strong>Deletion:</strong> delete your account in Settings → Profile.
        </li>
        <li>
          <strong>Nominate someone</strong> to act for you if you die or can't act yourself: email {mail}.
        </li>
        <li>
          <strong>Withdraw consent:</strong> delete your account.
        </li>
        <li>
          <strong>Grievances:</strong> email {mail}.
        </li>
      </ul>
      <p>
        For workspace content, ask the workspace's admins: they control it, and we act on their instructions. If you're not satisfied with our reply,
        you can complain to the Data Protection Board of India. EU and UK users can also complain to their local supervisory authority.
      </p>

      <h2>Who we share it with</h2>
      <p>
        We use a few service providers to run Taskwell; see the <Link to="/subprocessors">subprocessors</Link>. Integrations you turn on (Google
        sign-in, GitHub, GitLab, webhooks) receive data at your request. Your data may be processed outside India and the EU under those providers'
        safeguards, such as standard contractual clauses or the EU-US Data Privacy Framework.
      </p>

      <h2>Data breaches</h2>
      <p>
        If a breach affects your data, we notify you and the Data Protection Board of India without delay, with details within 72 hours. Where
        required we notify EU supervisory authorities within 72 hours, and we tell affected workspace admins without undue delay.
      </p>

      <h2>Age</h2>
      <p>Taskwell is for business use by people 18 or older. We don't knowingly collect data from children.</p>

      <h2>Changes</h2>
      <p>
        We'll update the date above when this notice changes and tell you by email about significant changes. See also the{' '}
        <Link to="/terms">terms of service</Link>.
      </p>
    </LegalPage>
  )
}
