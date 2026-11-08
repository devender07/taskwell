import { useEffect, useState, useTransition } from "react";
import { createFileRoute, useRouter } from "@tanstack/react-router";
import { Button } from "@taskwell/shared/button";
import { Dialog } from "@taskwell/shared/dialog";
import { formatMoney } from "@taskwell/shared/money";
import {
  PLAN_TAGLINES,
  PlanCard,
  PlanIncludes,
} from "@taskwell/shared/plan-card";
import { shortDate } from "@taskwell/shared/time";
import {
  api,
  billingPath,
  browserTZ,
  changePlan,
  openBillingPortal,
  plansPath,
  startCheckout,
  toFormErrors,
  type Billing,
  type Plan,
} from "@/api";
import { openCheckout } from "@/paddle";
import { trackPath } from "@/realtime";
import { FormError } from "@/components/auth/auth-card";
import { formatBytes } from "@/components/files/bytes";
import { planChecks } from "@/components/settings/plans";
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
  if (b.status === "past_due")
    return "Payment failed. Update your payment method.";
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
  const actions = billing.enabled && billing.can_manage && !billing.comped;
  const [error, setError] = useState<string | null>(null);
  const [pending, startTransition] = useTransition();
  const [switchTo, setSwitchTo] = useState<Plan | null>(null);
  const [confirming, setConfirming] = useState(false);

  useEffect(() => {
    if (!checkout) return;
    let n = 0;
    const id = setInterval(() => {
      router.invalidate();
      if (++n === 5) clearInterval(id);
    }, 3000);
    return () => clearInterval(id);
  }, [checkout, router]);

  const run = (task: () => Promise<void>) =>
    startTransition(async () => {
      setError(null);
      try {
        await task();
      } catch (e) {
        setError(toFormErrors(e).error);
      }
    });

  const upgrade = (p: Plan) =>
    run(async () => {
      const c = await startCheckout(workspace, p.id);
      await openCheckout(
        c,
        `${location.origin}${location.pathname}?checkout=done`,
      );
    });

  const confirmSwitch = (p: Plan) =>
    run(async () => {
      await changePlan(workspace, p.id);
      setConfirming(false);
      await router.invalidate();
    });

  const manage = () =>
    run(async () => location.assign((await openBillingPortal(workspace)).url));

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
      <ul className="plan-grid">
        {plans.map((p) => {
          const price = p.prices?.[billing.currency];
          const featured = p.id === "pro";
          return (
            <PlanCard
              key={p.id}
              name={p.name}
              tagline={PLAN_TAGLINES[p.id]}
              price={
                price === undefined
                  ? undefined
                  : price === 0
                    ? "Free"
                    : formatMoney(price, billing.currency)
              }
              featured={featured}
              checks={planChecks(p)}
            >
              {p.id === billing.plan ? (
                <p className="plan-current">Current plan</p>
              ) : !actions || p.id === "starter" ? null : (
                <Button
                  variant={featured ? "secondary" : "primary"}
                  disabled={pending}
                  onClick={() => {
                    if (!subscribed) return upgrade(p);
                    setSwitchTo(p);
                    setConfirming(true);
                  }}
                >
                  {subscribed ? "Switch plan" : "Upgrade"}
                </Button>
              )}
            </PlanCard>
          );
        })}
      </ul>
      {actions && subscribed ? (
        <Button
          variant="primary"
          style={{ justifySelf: "start" }}
          disabled={pending}
          onClick={manage}
        >
          Manage billing
        </Button>
      ) : null}
      {confirming ? null : <FormError>{error}</FormError>}
      <PlanIncludes />
      <Dialog
        open={confirming}
        onClose={() => {
          setConfirming(false);
          setError(null);
        }}
        title={`Switch to ${switchTo?.name ?? ""} now?`}
        description="You'll be charged the prorated difference today."
      >
        <div className="grid gap-4">
          <FormError>{error}</FormError>
          <Button
            variant="secondary"
            className="w-full"
            disabled={pending}
            onClick={() => switchTo && confirmSwitch(switchTo)}
          >
            {pending ? "Switching…" : "Switch plan"}
          </Button>
        </div>
      </Dialog>
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
