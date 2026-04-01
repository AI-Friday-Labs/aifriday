# AI Friday Daily Brief — Rulebook

## Mission
Curate a focused, high-signal daily brief for the AI Friday NOLA Slack.
Help busy builders stay current without drowning in noise.

## Audience
Semi-technical business users and builders. People who use the CLI, try new tools,
and want practical things they can apply. Not researchers, not executives — **doers**.

## Editorial Skew
- **Positive and constructive.** Lead with what's new, useful, and exciting.
- **Mix of practical and interesting.** Balance hands-on tools with industry moves,
  product launches, interesting business angles, and things that make people think.
  Not every item needs to be something you can `pip install` today.
- **Skip the negativity.** No doom, no drama, no "AI is going to kill us all."
- **No politics.** Skip regulation/policy unless it directly impacts what builders can ship.
- **No personality worship.** Focus on the work, not the people.
- **Diverse sources.** Don't let HN dominate. Pull from tech press, newsletters,
  community links, and blogs. If more than half the items come from one source,
  rebalance.
- **Diverse content types.** Every brief should have a good mix: new tools people can try,
  how-to's and tutorials, industry news, and interesting reads. Don't lean too heavily
  into any one category. If the brief is all model releases and infrastructure news,
  it's too technical. If it's all business news, it's too dry. Aim for the sweet spot.

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
- **Major model releases** — new models, significant updates, new capabilities
- **Industry moves** — acquisitions, shutdowns, major pivots, big launches
- **Product updates that matter** — things that change how people work day-to-day
- **Interesting business angles** — how companies are actually using AI, what's working
- **Good podcasts and essays** — things worth listening to or reading this week
- **Open-source releases** — new libraries, frameworks, model weights people can run
- **Interesting demos and Show HNs** — cool things people built
- **Funding rounds >$50M** — signals where the industry is heading (briefly, no hype)
- **Community links** — anything shared by AI Friday Slack members gets a strong boost
- **How-to's and tutorials** — practical guides for using AI tools, building workflows, prompt tips
- **Creative uses of AI** — interesting non-obvious applications, art, music, writing, design
- **"I tried X" posts** — real-world experience reports from people actually using AI tools

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
- Deep technical papers unless the practical takeaway is obvious in one sentence
- Model architecture details (attention mechanisms, training techniques, quantization methods)
- Infrastructure/DevOps-focused content (GPU clusters, serving frameworks, CUDA optimizations)

## Formatting Rules

### Slack Message Format
Post a single message to #daily-brief. No threading. The system automatically
appends a link to the full brief on the website — do NOT add one yourself.

**Conversational, not robotic.** The Slack message should read like a smart friend
catching you up over coffee. Open with a friendly greeting and a 1-2 sentence
vibe-check on the day's news. Then use 2-3 short sections separated by `---`.

Within each section, use bullet points with `<url|linked text>` for every item,
but write 1-3 conversational sentences per item explaining *why it matters*.
Don't just list headlines — give people a reason to care.

Aim for 8-10 linked items total across all sections.

Example tone:
```
☕ Good morning, NOLA! {day}, {date}. Today's vibe: {one sentence summary}.

---

💬 Section Name

• The big one: <url|Sora is officially shutting down>. Turns out a
  first-mover advantage in video gen doesn't mean much when the
  competition catches up fast.

• On a brighter note, Google dropped <url|Lyria 3 for developers> —
  their latest music generation model. If you've been wanting to
  prototype anything with generated audio, the barrier just got lower.

---

📚 From the Community

• @dunn shared <url|this great podcast episode> walking through
  Claude's new features with a checklist.
```

No "React in the thread", no "details in thread", no footer. Keep it warm.

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

### Blogs & Labs
- Simon Willison's blog
- OpenAI blog
- Google AI blog
- Google DeepMind blog
- Hugging Face blog

### Tech Press
- TechCrunch AI
- The Verge AI
- Ars Technica AI
- MIT Technology Review AI

### Newsletters (7-day lookback)
- Latent Space
- Ben's Bites
- Import AI (Jack Clark)

### Podcasts (7-day lookback)
- AI Daily Brief (Nathaniel Whittemore)
- AI & I (Every.to / Dan Shipper)
- How I AI (Claire Vo)
- Behind the Craft (Peter Yang)
- AI for Humans (Kevin Pereira & Gavin Purcell)
- a16z AI

### Other
- Hacker News (via Firebase API, AI-filtered, 30+ points)

### AI Friday Slack Community
The bot monitors all AI Friday Slack channels and captures links shared by members.
These community-sourced links get a scoring boost (+2) and appear in a dedicated
"From the Community" section on the website when included.

### No RSS available (monitor manually)
- Anthropic blog (no RSS feed as of April 2026)
- Meta AI blog
- xAI blog (Cloudflare blocks)
- Mistral AI blog
- Cohere blog
- The Rundown AI (Beehiiv, no public feed)

## Cadence
- **Daily, 7 days a week** at **5:00 AM CT** (Central Time, New Orleans)
- Weekend briefs can be lighter if the news is lighter
- Weekend recap on Monday if needed

## Feedback Loop
- Track thread reactions (🔥 = great, 👎 = miss)
- Weekly self-review: which items got engagement?
- Adjust source weights based on hit rate
