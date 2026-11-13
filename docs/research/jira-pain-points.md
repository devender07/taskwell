# What people dislike about Jira

Researched 2026-11-05 with web search and page fetches across Hacker News (via the Algolia API), Atlassian Community, G2 / Capterra / TrustRadius snippets, The Register, and dev blogs. Most sources are from 2023-2026. Reddit, G2, Capterra and TrustRadius pages blocked direct fetches, so those quotes come from search-result excerpts. Vendor blogs are flagged where they're used.

Frequency is judged by how often a theme came up across sources: **common** (nearly every source), **frequent** (several independent sources), **occasional** (a few).

## Ranked pain points

### Speed and performance

1. **Slow UI and page loads.** Issues, boards and transitions take seconds to load, and Cloud feels slower than the old self-hosted Server. *Common.*
   - HN, "I Fucking Hate Jira" thread (2024): Jira Cloud "is horrendously slow" ([39382644](https://news.ycombinator.com/item?id=39382644)). Another commenter mentions "installations that are slow AF" ([39422675](https://news.ycombinator.com/item?id=39422675)).
   - Atlassian Community (Jul 2024): after a UI update, "every click or keyboard input takes 4 or 5 seconds to register" ([thread](https://community.atlassian.com/t5/Jira-questions/Since-the-update-a-week-or-two-ago-Jirea-has-been-painfully-slow/qaq-p/2763666)). Also "Jira and Confluence (Cloud) is toooooo slow" ([thread](https://community.atlassian.com/forums/discussion/744373/jira-and-confluence-cloud-is-toooooo-slow)).
   - G2's review summary lists "lag and issues with ticket state transitions and overloaded boards" ([G2](https://www.g2.com/products/jira/reviews)).

2. **Large boards and backlogs bog down.** Big filters, many issues or many apps make boards laggy, and the official workaround is to show fewer issues. *Frequent.*
   - Atlassian Community: the UI has been "quite laggy since the ... board and backlog enhancements". An answer explains that boards run "many filters, many times" ([community](https://community.atlassian.com/t5/Jira-questions/Is-there-a-way-to-speed-up-the-user-interface/qaq-p/2568806)).
   - Intuit Engineering wrote about users calling Jira "extremely laggy" and set a target of under 3 seconds per response ([Medium](https://medium.com/intuit-engineering/revving-up-jira-proven-techniques-for-improved-performance-and-efficiency-c4756f224d5f)).

3. **Unreliability and outages.** Generic errors, failed moves and status-page incidents. *Occasional.*
   - HN (2023): "Jira has been consistently unreliable" ([37149154](https://news.ycombinator.com/item?id=37149154)). Also "I can't move a task to another column which gives me a non helpful generic error" ([37145613](https://news.ycombinator.com/item?id=37145613)).
   - Status history shows degraded issue creation and board loading in Sep 2026 ([pulsetic](https://pulsetic.com/status/jira-software/incidents/140981/)).

### Complexity and configuration

4. **Too flexible, so every instance turns into a mess.** Endless options and customization produce bloated, overcomplicated setups. Even defenders say "it's the configuration". *Common.*
   - A Reddit user quoted by AIM: "it's too flexible", which makes it unintuitive and a mess ([AIM](https://analyticsindiamag.com/why-developers-hate-jira/)).
   - HN (2025): "The fundamental problem with JIRA is that it's trying to be usable for all the workflows" ([44191265](https://news.ycombinator.com/item?id=44191265)). Also "We don't want fucking customized workflows" ([37145497](https://news.ycombinator.com/item?id=37145497)).
   - G2 summary: "steep learning curve and complex customization options" ([G2](https://www.g2.com/products/jira/reviews)).

5. **Steep learning curve for new and non-technical users.** Reviewers call it overwhelming at first and too complex for what small teams need. *Common.*
   - Capterra: it "can sometimes feel overly complex, especially for new users" ([Capterra](https://www.capterra.com/p/19319/JIRA/reviews/?page=12)).
   - TrustRadius (Mar 2025): "Awesome feature set with a sometimes challenging interface", and "It is improving but not fast enough" ([TrustRadius](https://www.trustradius.com/reviews/atlassian-jira-2025-03-14-03-52-35)).

6. **Admin scheme sprawl.** Permission, workflow, screen, field-configuration and notification schemes are layered per project and per issue type, so admins lose track of who can do what. *Frequent (among admins).*
   - Atlassian's own feature request says permission schemes are "already very granular and complex", with dozens of permissions to learn ([JRACLOUD-42116](https://jira.atlassian.com/browse/JRACLOUD-42116)).
   - Community: schemes copied so often that no one knows who has access ([community](https://community.atlassian.com/forums/discussion/2941917/10-most-common-challenges-every-jira-admin-faces-daily-and-how-to-solve-them)). New users name field configuration and screen schemes the hardest part to learn ([community](https://community.atlassian.com/forums/New-to-Jira-discussions/What-has-been-the-hardest-thing-for-you-when-you-started-working/td-p/1768812)).

7. **Locked-down workflows that devs can't change.** Developers are stuck with workflows imposed by admins or managers that don't match their work. *Frequent.*
   - HN (2024): "locked into overly complicated workflows that didn't match what I needed" ([39378381](https://news.ycombinator.com/item?id=39378381)). Also "I've never been able to edit the workflow" ([39378439](https://news.ycombinator.com/item?id=39378439)).

### UX and navigation

8. **Cluttered UI and too many clicks.** Simple tasks take several clicks, and screens are crowded with fields. *Common.*
   - HN (2025): "Jira's a great tool with a terrible UI" ([44200677](https://news.ycombinator.com/item?id=44200677)). Also "Jira is also just terribly built" ([44188237](https://news.ycombinator.com/item?id=44188237)).
   - HN (older): "I need to click probably at least 5 or 6 things to do something" ([9973119](https://news.ycombinator.com/item?id=9973119)).

9. **The 2025 navigation redesign.** The new sidebar is cramped, buries key functions, adds clicks, and has no opt-out. *Frequent (2025-2026).*
   - Atlassian Community threads include "Keep the old navigation (permanent opt-out)" ([article](https://community.atlassian.com/forums/Navigation-Refresh-articles/Keep-the-old-navigation-permanent-opt-out/ba-p/3003036)), "Jira 2025 changes: how user experience has degraded – is Atlassian ignoring user feedback?" ([thread](https://community.atlassian.com/forums/Jira-Cloud-Admins-discussions/Jira-2025-Changes-How-User-Experience-has-degraded-Is-Atlassian/m-p/3194686)), and a public opt-out request ([JRACLOUD-94450](https://jira.atlassian.com/browse/JRACLOUD-94450)).
   - Search excerpts from these threads report the layout is "unusable on a laptop monitor" and that moving between epics, backlog and sprints loses context.

10. **Rich text editor fights you.** Wiki markup and markdown are split between the create screen and the issue view, auto-formatting mangles pasted text, and some reported bugs destroy content. *Frequent.*
    - HN (2023): "Their rich text editor has consistently sucked and has destructive bugs in it" ([37149154](https://news.ycombinator.com/item?id=37149154)).
    - Atlassian Community: "How to switch off this damn auto text formatting" ([thread](https://community.atlassian.com/forums/Jira-questions/How-to-switch-off-this-damn-auto-text-formatting/qaq-p/1653441)). Formatting is lost in descriptions ([thread](https://community.atlassian.com/t5/Jira-questions/Is-anyone-else-getting-weird-format-losses-in-the-Description/qaq-p/2519818)). Create-screen markup doesn't render ([JRACLOUD-71890](https://jira.atlassian.com/browse/JRACLOUD-71890)).

### Workflow and process overhead

11. **Built for managers, not for the people doing the work.** Jira serves reporting and dashboards for leadership, while devs pay the data-entry cost. *Common.*
    - HN (2024): the tool choice "is almost always imposed by non-users of those tools" ([39382640](https://news.ycombinator.com/item?id=39382640)). Also "delivers sweet, sweet dashboards to upper managers" ([39383027](https://news.ycombinator.com/item?id=39383027)).
    - In The Pragmatic Engineer 2025 survey (3,000+ responses), Jira was the most disliked tool. Gergely Orosz speculated that devs who like tickets move into PM roles ([elezea summary](https://elezea.com/2025/07/the-pragmatic-engineer-2025-survey-whats-in-your-tech-stack/), [Product Hunt discussion](https://www.producthunt.com/p/general/opinions-on-the-1-disliked-tool-pragmatic-engineer-2025-survey)).

12. **Enables micromanagement and ceremony.** Story points, sprint rituals and per-person tracking feel like surveillance and get in the way of work. *Frequent.*
    - HN: "Jira is an attractive nuisance for micro-Managers" ([37146486](https://news.ycombinator.com/item?id=37146486)). Also "the whole 'agile' and Jira thing just wastes my time" ([39378988](https://news.ycombinator.com/item?id=39378988)) and "It is horrible. I just want to work." ([39380079](https://news.ycombinator.com/item?id=39380079)).

13. **Tickets drift from reality.** Work happens in git and Slack, so Jira goes stale unless someone updates it by hand. *Occasional.*
    - AIM, summarizing Reddit: devs "update code, push changes, have Slack conversations - all without touching Jira" ([AIM](https://analyticsindiamag.com/why-developers-hate-jira/)).
    - HN: "a management's devotion to Jira over the tools that actually get things done" ([37149909](https://news.ycombinator.com/item?id=37149909)).

14. **Weak at simple kanban.** People find it heavy for plain flow-based work. *Occasional.*
    - HN: "Jira is awful at Kanban" ([37150088](https://news.ycombinator.com/item?id=37150088)).

### Search / JQL

15. **Search is either too basic or needs JQL.** Basic search is weak and has no fuzzy matching. JQL is daunting for non-devs, field names are opaque, and results are sometimes wrong or slow. *Frequent.*
    - Atlassian Community article: unknown field names are the hardest part of finding work items ([article](https://community.atlassian.com/forums/New-to-Jira-articles/Finding-things-with-JQL-in-Jira/ba-p/3083059)). There's also a "biggest JQL frustrations" thread ([discussion](https://community.atlassian.com/forums/App-Central-discussions/What-are-your-biggest-JQL-frustrations-and-how-do-you-solve-them/m-p/3220098)).
    - Capterra reviewers mention "unclear search results" and slow search on older tickets ([Capterra](https://www.capterra.com/p/19319/JIRA/reviews/?page=12)).

### Notifications

16. **Notification flood, yet the important ones get missed.** Defaults spam the inbox, filtering is coarse, and critical changes get buried. *Frequent.*
    - Capterra: "The default notification settings can flood your inbox unless fine-tuned". Another reviewer says that when several projects update at once, "important notifications get lost" ([Capterra](https://www.capterra.co.uk/reviews/19319/jira)).
    - Some devs get "40+ notifications per day" and none when something critical changes (GitScrum, a vendor page: [link](https://gitscrum.com/en/solutions/pains/why-developers-hate-jira-and-what-to-use-instead)).

### Pricing and licensing

17. **Expensive, with yearly price hikes.** Cloud list prices rose 5-10% in Oct 2025, after Data Center prices went up 15-25% earlier in 2025. Small teams find it expensive for what they use. *Frequent.*
    - Atlassian FY26 pricing notice ([PDF](https://s206.q4cdn.com/270053503/files/doc_downloads/2025/09/Fiscal-2026-Cloud-Pricing-notice.pdf)). Community thread "Massive price increase for Jira Cloud Enterprise and apps?" cites one app going from $2,438 to $8,125/yr ([thread](https://community.atlassian.com/forums/Jira-questions/Massive-Price-Increase-for-Jira-Cloud-Enterprise-and-Apps/qaq-p/2969785)).
    - Capterra: "expensive, especially for smaller teams or startups".

18. **The marketplace "plugin tax".** Basic needs such as better reporting, checklists, scripting and time tracking require paid apps, priced per user across the whole instance. *Frequent.*
    - G2 reviewer: for "certain basic features or advanced reporting, you often have to rely on and pay for third-party marketplace add-ons" ([G2](https://g2.com/products/jira/reviews_and_filters?page=4)).
    - Atlassian Community: an app costing "$2 per user, costs us $100 p/m for a 50-person team" ([thread](https://community.atlassian.com/t5/Jira-discussions/Why-are-plugins-priced-based-on-total-Jira-users-instead-of/m-p/1427248)).

### Atlassian cloud migration

19. **Forced move to Cloud.** Server ended in 2024. Data Center stops new sales in Mar 2026 and reaches end of life on Mar 28, 2029. Customers with deep integrations say Cloud doesn't work for them and face a second migration. *Frequent (self-hosted customers).*
    - The Register (Sep 2025): "we cannot migrate to cloud (tested this a lot and it does not work)". Also "a relatively abrupt end-of-life announcement is frustrating". A consultant estimates about 28% higher cost ([The Register](https://www.theregister.com/2025/09/09/atlassian_will_go_cloudonly_customers/)).

20. **Access control and auth friction.** Atlassian account and SSO layers get in the way. *Occasional.*
    - HN (2024): "the access control/authentication that atlassian puts on top of it is total dogshit" ([39401312](https://news.ycombinator.com/item?id=39401312)).

### Mobile

21. **Second-tier mobile app.** Features are missing, dashboards are broken, some attachments can't be viewed, Forge apps don't work, and the web fallback isn't mobile-friendly. *Occasional.*
    - App-store review aggregates call it "a second tier interface" and say it is "sadly lacking in features" ([justuseapp](https://justuseapp.com/en/app/1006972087/jira-cloud-by-atlassian/reviews), [Kimola report](https://kimola.com/reports/unlock-insights-jira-cloud-user-feedback-analysis-report-app-store-us-144594)).

## What people praise

- **Power and flexibility.** Jira can model almost any process, and G2 still averages about 4.3/5 across thousands of reviews ([G2](https://www.g2.com/products/jira/reviews)).
- **It is good when kept simple.** "If you limit Jira to just an issue tracker with Kanban it's actually pretty good" ([HN 39383693](https://news.ycombinator.com/item?id=39383693)). Many blame bad configuration, not the tool ([HN 39382261](https://news.ycombinator.com/item?id=39382261)).
- **Reporting and dashboards for leadership.** PMs miss Jira's reporting after switching to Linear ([Cotera](https://cotera.co/articles/linear-vs-jira-comparison.md)).
- **Ecosystem and integrations.** It "does integrate pretty deeply with source control" ([HN 37151752](https://news.ycombinator.com/item?id=37151752)), and the Marketplace covers almost any gap.
- **Self-hosted Server and Data Center were liked.** "pretty good products that do their thing fine" ([HN 39382644](https://news.ycombinator.com/item?id=39382644)).
- **Teams that switch to Linear mostly cite speed and less friction.** Gaps they report after switching are time tracking and reporting ([Cotera](https://cotera.co/articles/linear-vs-jira-comparison.md), [Yummygum](https://blog.yummygum.com/blog/moving-to-linear-for-project-management)).

## Opportunities for Taskwell

Coverage is a best guess, based only on Taskwell's planned feature list, not on testing the product.

| # | Pain point | How a lightweight, fast, keyboard-first tool avoids it | Coverage |
|---|---|---|---|
| 1 | Slow UI | Instant optimistic updates, preloaded routes, and a hard latency budget per interaction | Partly covered (depends on execution) |
| 2 | Large boards lag | Paginated or virtualized lists and server-side filters, with no per-board filter stacks | Partly covered |
| 3 | Unreliability, generic errors | Clear sentence-case error messages that say what to do next, and status shown in-app | Gap |
| 4 | Too flexible / config mess | Opinionated defaults: fixed issue fields, one issue type, no custom fields or automation builder | Already covered (by design) |
| 5 | Steep learning curve | Usable with zero setup, and the command palette makes features discoverable | Already covered |
| 6 | Admin scheme sprawl | One workspace-level custom-roles model instead of per-project schemes, plus guests | Already covered |
| 7 | Locked-down workflows | Simple per-team statuses that the team edits itself | Partly covered |
| 8 | Cluttered UI, many clicks | Keyboard shortcuts for every action, quick-create, inline edits | Already covered |
| 9 | Disruptive redesigns | Stable navigation; ship UI changes gradually, with an opt-out | Gap (process, not feature) |
| 10 | Bad editor | Live collaborative editing with markdown shortcuts and a single editor everywhere | Already covered |
| 11 | Built for managers | Dev-first issue flow, with roadmap and cycles as a light layer on top | Partly covered |
| 12 | Micromanagement | No per-person points leaderboard; cycles focus on team progress | Partly covered |
| 13 | Tickets drift from reality | GitHub/GitLab PR linking that moves status automatically; chat next to issues | Already covered |
| 14 | Heavy kanban | Kanban is a core view, not a configured board | Already covered |
| 15 | Search / JQL | Fast fuzzy search plus URL-backed filters, with no query language to learn | Already covered |
| 16 | Notification flood | Mention-driven notifications, an in-app inbox, email digests, per-event settings | Already covered |
| 17 | Price hikes | Simple, predictable per-seat pricing | Gap (not in feature list) |
| 18 | Plugin tax | Core needs built in: PR linking, roadmap, feedback intake, API and webhooks | Partly covered |
| 19 | Forced cloud migration | A Jira importer for teams leaving Data Center before 2029 | Gap |
| 20 | Auth friction | Magic link and Google sign-in, with no separate account layer | Already covered |
| 21 | Weak mobile | Responsive web at 640px breakpoints with 44px touch targets | Partly covered |

The biggest gaps worth considering: a **Jira importer**, timed for the Data Center end of life, and **clear, actionable error states**. Pricing transparency is a business decision rather than a feature.
