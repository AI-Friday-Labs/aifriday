# Design System — AI Friday

## Product Context
- **What this is:** Website + Slack bot for a New Orleans AI meetup group
- **Who it's for:** Startup founders and operators figuring out AI
- **Space:** Community / meetup — not SaaS, not a product
- **Project type:** Editorial site with meeting notes, daily briefs, and newsletter signup

## Aesthetic Direction
- **Direction:** Editorial/Magazine meets Organic/Natural
- **Decoration level:** Intentional — subtle grain texture, no illustrations or icons
- **Mood:** Friday morning coffee shop. Warm, unhurried, genuine. The opposite of a tech conference.
- **Reference:** Daring Fireball, Craig Mod's Roden, a good indie zine

## Typography
- **Display/Hero:** Fraunces (variable, opsz 9-144) — warm, a little weird, confident. The soul of the brand.
- **Body:** Plus Jakarta Sans — clean, modern, doesn't compete with Fraunces
- **Code/Meta:** JetBrains Mono — for dates, labels, section markers
- **Loading:** Google Fonts
  ```
  Fraunces:ital,opsz,wght@0,9..144,300;0,9..144,400;0,9..144,500;0,9..144,600;0,9..144,700;0,9..144,800;1,9..144,400;1,9..144,500
  Plus+Jakarta+Sans:ital,wght@0,300;0,400;0,500;0,600;0,700;1,400
  JetBrains+Mono:wght@400;500
  ```
- **Scale:**
  - Hero: clamp(28px, 5vw, 44px) — editorial, not billboard
  - H1: 28px
  - H2: 22px (Fraunces, weight 500)
  - H3: 18px (Fraunces, weight 500)
  - Body large: 17px (hero subtitle, prose)
  - Body: 15px
  - Small/Meta: 13px
  - Labels: 11px (JetBrains Mono, uppercase, tracked)

## Color
- **Approach:** Restrained — one accent + warm neutrals
- **Palette:**
  - Background: `#FDF6EE` (warm cream — feels like good paper)
  - Surface: `#FFFFFF` (white, for cards when needed)
  - Text: `#2C1810` (deep espresso)
  - Text secondary: `#5A3D2E`
  - Muted: `#8B7355` (warm taupe — dates, meta, labels)
  - Accent: `#D4663A` (terracotta — CTAs, links, emphasis)
  - Accent hover: `#C05A32`
  - Secondary: `#5B8A72` (sage green — brief links, subtle variety)
  - Border: `#E8DDD0`
  - Border subtle: `#F0E6D8`

## Spacing
- **Base unit:** 4px
- **Density:** Spacious — editorial, not dashboard
- **Scale:** xs(4) sm(8) md(16) lg(24) xl(40) 2xl(64) 3xl(96)

## Layout
- **Approach:** Editorial — prose-first, not component-first
- **Max content width:** 680px
- **Border radius:** sm: 4px, md: 6px, lg: 8px (restrained — no bubbly)
- **Cards:** Use sparingly. Only for next-meeting callout and brief items. Homepage is open prose on cream background, not boxed sections.
- **No footer.** Pages just end.

## Motion
- **Approach:** Minimal-functional
- **Rule:** No entrance animations, no scroll effects. The page loads, it's there.

## Texture
- Subtle SVG grain overlay at 3% opacity on body — adds warmth
- No other decorative elements

## Page Structure

### Homepage (`/`)
1. Nav: AI Friday wordmark + Meetings / Briefs links
2. Headline + two paragraphs of prose (no cards, no sections)
3. CTA row: "Join us on Slack" button + "Next meeting" link
4. Terracotta divider (40px, centered)
5. Next meeting card (white surface, border) with RSVP questions
6. Today's brief preview: lede paragraph + item list + "Read full brief" link
7. Newsletter signup: one line of copy + email input + subscribe button

### Meetings index (`/meetings`)
- Simple list: number, title, date, attendance count
- Fraunces for meeting titles

### Meeting detail (`/meetings/:id`)
- Title, date/time/location in mono
- Prose summary of what happened
- Attendee pills (border-subtle background, rounded)
- Demos & discussion as prose paragraphs
- Links mentioned list with who shared them

### Daily brief (`/brief/YYYY/MM/DD`)
- Lede paragraph with inline links (existing format — working well)
- Sections with items
- Sources footer
- Prev/next navigation

## Section Labels
Use JetBrains Mono, 11px, uppercase, letter-spacing 0.1em, muted color.
Examples: `NEXT MEETING`, `TODAY'S BRIEF · MARCH 25, 2026`, `STAY IN THE LOOP`

## Anti-patterns — never do these
- Three-column feature grids or value-prop cards
- Purple/violet gradients
- Centered everything with uniform spacing
- Generic hero sections with stock imagery
- Bold-first bullet point lists as page structure
- Footer with fleur-de-lis (removed)
- Any pattern that reads as "SaaS landing page"

## Voice Reference
See `VOICE.md` for full voice and tone guide. Key rule: the site should
sound like you're explaining the group to someone at a coffee shop.

## Decisions Log
| Date | Decision | Rationale |
|------|----------|----------|
| 2026-03-25 | Initial design system | Created via /design-consultation based on existing site + voice guide |
| 2026-03-25 | Reduced hero size from 80px to 44px max | Editorial, not billboard |
| 2026-03-25 | Removed value-prop cards from homepage | Read as SaaS landing page, contradicted voice |
| 2026-03-25 | Removed footer | Unnecessary — pages just end |
| 2026-03-25 | Added meeting notes + RSVP form to site structure | Per user: meetings are the core product |
| 2026-03-25 | CTA: "Join us on Slack" not "Join the Slack" | Warmer, about people not product |
