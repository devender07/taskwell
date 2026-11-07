import { useActionState, useEffect } from "react";
import { createFileRoute, useRouter } from "@tanstack/react-router";
import { Button } from "@taskwell/shared/button";
import { formatMoney } from "@taskwell/shared/money";
import { shortDate } from "@taskwell/shared/time";
import {
  api,
  billingPath,
  browserTZ,
  openBillingPortal,
  plansPath,
  startCheckout,
  toFormErrors,
  type Billing,
  type Plan,
  type PlanId,
} from "@/api";
import { trackPath } from "@/realtime";
import { FormError } from "@/components/auth/auth-card";
import { formatBytes } from "@/components/files/bytes";
import "@/components/home/home.css";

type Search = { checkout?: "done" };

export const Route = createFileRoute("/_app/$workspace/_main/settings/billing")(
  {
    validateSearch: (search): Search => ({
      checkout: search.checkout === "done" ? "done" : undefined,
    }),
    loader: async (ctx) => {
      const path = billingPath(ctx.params.workspace);
      trackPath(ctx, path);
      const [billing, { plans }] = await Promise.all([
        api<Billing>(`${path}?tz=${encodeURIComponent(browserTZ())}`),
        api<{ plans: Plan[] }>(plansPath),
      ]);
      return { billing, plans };
    },
    component: BillingSettings,
  },
);

const SUBSCRIBED = ["active", "trialing", "past_due"];

function statusLine(b: Billing) {
  if (b.comped) return "Comped by Taskwell";
  if (b.status === "past_due") return "Payment failed. Update your card.";
  if (!b.renews_at) return "";
  return b.cancel_at_period_end
    ? `Cancels on ${shortDate(b.renews_at)}`
    : `renews on ${shortDate(b.renews_at)}`;
}

function BillingSettings() {
  const { workspace } = Route.useParams();
  const { checkout } = Route.useSearch();
  const { billing, plans } = Route.useLoaderData();
  const router = useRouter();
  const subscribed = SUBSCRIBED.includes(billing.status ?? "");
  const actions = billing.enabled && billing.can_manage;

  useEffect(() => {
    if (!checkout) return;
    let n = 0;
    const id = setInterval(() => {
      router.invalidate();
      if (++n === 5) clearInterval(id);
    }, 3000);
    return () => clearInterval(id);
  }, [checkout, router]);

  const [error, act, pending] = useActionState(
    async (_prev: string | null, form: FormData) => {
      try {
        const plan = form.get("plan") as PlanId | null;
        const { url } = plan
          ? await startCheckout(workspace, plan)
          : await openBillingPortal(workspace);
        location.assign(url);
        return null;
      } catch (e) {
        return toFormErrors(e).error;
      }
    },
    null,
  );

  const current = plans.find((p) => p.id === billing.plan);
  const line = statusLine(billing);

  return (
    <main
      className="grid gap-6 w-full px-8 py-10"
      style={{ alignContent: "start" }}
    >
      <header className="page-top grid gap-2">
        <h1 className="text-h2">Billing</h1>
        <p className="text-body-sm text-secondary">
          Plans are priced per workspace, not per person.
        </p>
      </header>
      {checkout ? (
        <p role="status" className="text-body-sm text-success m-0">
          Thanks. Your plan updates in a few seconds.
        </p>
      ) : null}
      {billing.enabled ? null : (
        <p className="text-body-sm text-secondary m-0">
          Billing isn't set up yet.
        </p>
      )}
      <section
        className="grid gap-4 p-6 bg-surface border rounded-lg"
        aria-labelledby="plan-title"
      >
        <h2 id="plan-title" className="text-label">
          {current?.name ?? billing.plan}
          {line ? (
            <span
              className={
                billing.status === "past_due" ? "text-accent" : "text-secondary"
              }
            >
              {" "}
              · {line}
            </span>
          ) : null}
        </h2>
        <Meter
          label="Members"
          used={billing.members.used}
          limit={billing.members.limit}
          format={String}
        />
        <Meter
          label="Storage"
          used={billing.storage.used_bytes}
          limit={billing.storage.limit_bytes}
          format={formatBytes}
        />
      </section>
      <form action={act} className="grid gap-4">
        <ul className="plan-grid">
          {plans.map((p) => {
            const isCurrent = p.id === billing.plan;
            const price = p.prices?.[billing.currency];
            return (
              <li
                key={p.id}
                className="plan-card grid gap-3 p-6 bg-surface border rounded-lg"
                aria-current={isCurrent || undefined}
                style={{ alignContent: "start" }}
              >
                <h3 className="text-label m-0">{p.name}</h3>
                {price === undefined ? null : (
                  <p className="m-0">
                    <span className="text-h2">
                      {price === 0
                        ? "Free"
                        : formatMoney(price, billing.currency)}
                    </span>
                    {price === 0 ? null : (
                      <span className="text-body-sm text-secondary">
                        {" "}
                        / month
                      </span>
                    )}
                  </p>
                )}
                <p className="text-body-sm text-secondary m-0">
                  {p.members === null
                    ? "Unlimited members"
                    : `Up to ${p.members} members`}
                  <br />
                  {p.storage_bytes === null
                    ? "Unlimited storage"
                    : `${formatBytes(p.storage_bytes)} storage`}
                </p>
                {isCurrent ? (
                  <p className="text-label text-secondary m-0">Current plan</p>
                ) : !actions ||
                  billing.comped ||
                  p.id === "starter" ? null : subscribed ? (
                  <Button
                    type="submit"
                    variant="primary"
                    style={{ justifySelf: "start" }}
                    disabled={pending}
                  >
                    Switch plan
                  </Button>
                ) : (
                  <Button
                    type="submit"
                    name="plan"
                    value={p.id}
                    variant="secondary"
                    style={{ justifySelf: "start" }}
                    disabled={pending}
                  >
                    Upgrade
                  </Button>
                )}
              </li>
            );
          })}
        </ul>
        {actions && subscribed ? (
          <Button
            type="submit"
            variant="primary"
            style={{ justifySelf: "start" }}
            disabled={pending}
          >
            Manage billing
          </Button>
        ) : null}
        <FormError>{error}</FormError>
      </form>
    </main>
  );
}

function Meter({
  label,
  used,
  limit,
  format,
}: {
  label: string;
  used: number;
  limit: number | null;
  format: (n: number) => string;
}) {
  const text =
    limit === null
      ? `${format(used)} · Unlimited`
      : `${format(used)} of ${format(limit)}`;
  return (
    <div className="grid gap-2" style={{ maxWidth: 400 }}>
      <span className="flex justify-between text-body-sm">
        <span>{label}</span>
        <span className="text-secondary">{text}</span>
      </span>
      {limit === null ? null : (
        <span
          role="progressbar"
          aria-label={`${label} used`}
          aria-valuemin={0}
          aria-valuemax={limit}
          aria-valuenow={used}
          aria-valuetext={text}
          className="home-bar"
        >
          <span
            style={{
              width: limit ? `${Math.min(100, (used / limit) * 100)}%` : 0,
            }}
          />
        </span>
      )}
    </div>
  );
}
