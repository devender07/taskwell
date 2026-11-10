import { Link, createFileRoute } from '@tanstack/react-router'
import { LegalPage } from '@/components/legal/legal-page'

export const Route = createFileRoute('/terms')({
  component: Terms,
})

function Terms() {
  return (
    <LegalPage title="Terms of service">
      <p>
        These terms are an agreement between you (and the organization you use Taskwell for) and [Company legal name], [Registered address, India]
        ("we"). By creating an account or using Taskwell you accept them and the <Link to="/privacy">privacy notice</Link>.
      </p>

      <h2>Your account</h2>
      <ul>
        <li>You must be 18 or older and use Taskwell for business or professional purposes.</li>
        <li>Give accurate details, keep your sign-in methods secure and tell us at privacy@taskwell.dev if you think your account was misused.</li>
        <li>You're responsible for what happens under your account. Workspace admins control who can join and what members can do.</li>
      </ul>

      <h2>Acceptable use</h2>
      <p>Don't use Taskwell to:</p>
      <ul>
        <li>break the law, or infringe anyone's rights;</li>
        <li>upload malware, or try to get into accounts, workspaces or systems that aren't yours;</li>
        <li>send spam, or overload, probe or disrupt the service;</li>
        <li>resell the service or copy it to build a competing product.</li>
      </ul>
      <p>We may suspend accounts that break these rules, and will tell you why unless the law or security stops us.</p>

      <h2>Your content</h2>
      <p>
        Everything a workspace puts into Taskwell (issues, comments, chat, files) belongs to the workspace. You give us only the permission we need
        to host, process and show it to provide the service. We process it as your processor under our data processing agreement, available on
        request at privacy@taskwell.dev.
      </p>

      <h2>Fees</h2>
      <p>
        Paid plans are sold through Paddle, our merchant of record, which handles payment, taxes and invoices under its own terms. Prices are on the{' '}
        <Link to="/pricing">pricing page</Link>. Plans renew until you cancel; after cancelling you keep the plan until the end of the period you
        paid for.
      </p>

      <h2>Ending the agreement</h2>
      <p>
        You can stop at any time: workspace admins can export a workspace, you can download your own data and delete your account in Settings →
        Profile. We may end the agreement if you seriously break these terms, or with 30 days' notice if we stop the service, giving you time to
        export your data.
      </p>

      <h2>Disclaimers</h2>
      <p>
        We work hard to keep Taskwell available and secure, but the service is provided "as is" without warranties beyond those the law requires.
        Keep your own copies of anything critical.
      </p>

      <h2>Liability</h2>
      <p>
        Neither party is liable for indirect or consequential losses, such as lost profits or data. Our total liability for any claim is limited to
        the fees you paid us in the 12 months before the claim. Nothing here limits liability that the law doesn't allow to be limited.
      </p>

      <h2>Changes</h2>
      <p>We'll update the date above when these terms change and tell you by email about significant changes before they apply.</p>

      <h2>Governing law</h2>
      <p>These terms are governed by the laws of [India, courts of ___], which have exclusive jurisdiction.</p>

      <p>
        See also the <Link to="/subprocessors">subprocessors</Link> we use.
      </p>
    </LegalPage>
  )
}
