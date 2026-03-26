# AI Friday Daily Brief — Rulebook

## Mission
Curate a focused, high-signal daily brief for the AI Friday NOLA Slack.
Help busy builders stay current without drowning in noise.

## Audience
Semi-technical business users and builders. People who use the CLI, try new tools,
and want practical things they can apply. Not researchers, not executives — **doers**.

## Editorial Skew
- **Positive and constructive.** Lead with what's new, useful, and exciting.
- **Tools-first.** Prioritize things people can try, install, or build with today.
- **Skip the negativity.** No doom, no drama, no "AI is going to kill us all."
- **No politics.** Skip regulation/policy unless it directly impacts what builders can ship.
- **No personality worship.** Focus on the work, not the people.

## Accessibility & Tone Calibration

The audience uses AI tools daily but doesn't train models or manage GPU clusters.
Every item should pass these filters before publishing:

- **Jargon check.** If a term wouldn't be understood by someone who uses ChatGPT
  and the CLI but has never fine-tuned a model, either explain it in ≤5 words inline
  or skip it.
  - ❌ Needs explanation: MoE, quantization, VRAM, embeddings, ASGI, CRDT,
    unified memory architecture, mechanistic interpretability, attention heads,
    MCP (first mention), GGUF, LoRA
  - ✅ Fine as-is: API, open source, CLI, model, prompt, fine-tune, chatbot, GitHub,
    token, plug-in, agent, app

- **Lead with outcomes, not architecture.** The reader wants to know what they can
  *do*, not how it works under the hood.
  - ✅ "Search video in under a second"
  - ❌ "Native video embeddings over the actual video signal"
  - ✅ "Run big AI models on a normal laptop"
  - ❌ "Streaming MoE expert weights from SSD"

- **The cofounder test.** Before including an item, ask: *"Would my non-engineer
  cofounder find this interesting?"* If no, it's either too niche or needs
  reframing. This doesn't mean dumbing down — it means finding the angle that
  matters to people who build products, not just people who build infrastructure.

- **De-prioritize plumbing.** Local inference tuning, model internals, attention
  mechanism papers, kernel-level code review tools — these belong in the "Also"
  quick links at most. They shouldn't be full items unless the practical payoff
  is obvious to anyone (e.g., "You can now run GPT-4-class models on your laptop
  for free").

- **One technical detail max per item.** If you need to explain *why* something
  is cool, you get one technical detail. Make it count. The rest should be about
  what it means for the reader.

## What Makes the Cut

### ✅ INCLUDE
- **New AI tools and products** — especially ones you can try right now
- **CLI tools and developer utilities** — MCP servers, agents, coding assistants, workflow tools
- **Major model releases** — new models, significant updates, new capabilities
- **Open-source releases** — new libraries, frameworks, model weights people can run
- **Practical tutorials** — that teach something actionable in <30 min
- **Interesting demos and Show HNs** — cool things people built
- **API & platform changes** — new endpoints, pricing changes that affect builders
- **Funding rounds >$50M** — signals where the industry is heading (briefly, no hype)

### ❌ EXCLUDE
- Hype pieces with no substance ("AI will change everything")
- Negative/doom content ("AI taking all jobs", existential risk debates)
- Politics, regulation, policy, government stuff
- Drama / Twitter beefs / personality gossip
- Listicles rehashing old tools
- Paywalled content with no free summary
- Incremental updates (minor version bumps, small bug fixes)
- Rumor-only stories with no credible sourcing
- Crypto/Web3 projects that slapped "AI" on the label
- Opinion pieces about whether AI is good or bad

## Formatting Rules

### Daily Brief Structure
Post one summary message to the channel, then each item as a reply in the thread.

**Main message:**
```
🌞 AI Friday Daily Brief — {date}

{count} items today • React in the thread 🔥👎

💥 Big Moves (0-2 items)
• headline summaries

🛠️ Tools & Releases (2-5 items)
• headline summaries

📚 Worth a Look (1-3 items)
• headline summaries
```

**Thread replies (one per item):**
```
🛠️ [Tool] **Tool Name**
One-liner description.
→ Why it's interesting (one line)
🔗 link
```

This lets people react (🔥/👎) to individual items without cluttering the channel.

### NOLA Spotlight
Include a `🎺 NOLA Spotlight` section only when there's genuinely something local.
Don't force it — most days there won't be anything. That's fine.

### HTML Body Rules
- Use `<strong>`, `<a href>`, `<code>` in item bodies
- For direct quotes, use `<blockquote>` as a **block element** (not inline)
- Never put `<blockquote>` inside a sentence — use curly quotes \u201c...\u201d instead
- Keep HTML simple: no `<div>`, `<span>`, or nested structures in body text

## HN Point Counts
Do not display Hacker News point counts in the brief. Points are used internally
for story selection and ranking, but they add noise for readers and age poorly.
Say "popular on HN" or link to the discussion — never cite a specific number.

## Scoring (internal, not shown to users)
Each candidate item gets scored 0-10:
- Novelty (0-3): Is this actually new?
- Usefulness (0-3): Can someone do something with this today?
- Coolness (0-2): Would this make someone say "oh that's cool"?
- Credibility (0-2): Is the source reliable?
- Community boost (+2): Item was shared by an AI Friday Slack member

**Threshold: 5+ to include.** Max 12 items per daily brief.

Items from the AI Friday Slack community get an automatic +2 scoring boost.
These are things real members found interesting enough to share — that signal matters.

## Tone
- Upbeat and practical
- Conversational but not sloppy
- Brief enthusiasm when warranted ("this is legit cool")
- No corporate speak, no breathless hype
- Emoji are fine, don't overdo it
- Write for people who build things

## Sources

### RSS Feeds
- Simon Willison's blog
- OpenAI blog
- Google AI blog
- Hugging Face blog
- Hacker News (via Firebase API, AI-filtered, 30+ points)

### Email Newsletters
- TLDR AI
- Ben's Bites
- The Neuron
- The Rundown AI
- TAAFT (There's An AI For That)
- FutureTools
- Lenny's Newsletter

### AI Friday Slack Community
The bot monitors all AI Friday Slack channels and captures links shared by members.
These community-sourced links get a scoring boost (+2) and appear in a dedicated
"From the Community" section on the website when included.

### Future additions
- Anthropic blog (need correct RSS URL)
- ArXiv (cs.AI, cs.CL) — only notable papers
- Latent Space podcast/newsletter
- Import AI newsletter

## Cadence
- **Daily, 7 days a week** at **7:00 AM CT** (Central Time, New Orleans)
- Weekend briefs can be lighter if the news is lighter
- Weekend recap on Monday if needed

## Feedback Loop
- Track thread reactions (🔥 = great, 👎 = miss)
- Weekly self-review: which items got engagement?
- Adjust source weights based on hit rate
