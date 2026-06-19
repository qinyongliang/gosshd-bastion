# GOSSHD Bastion I18N Redesign Brief

## Goal

Create a bilingual product experience that follows the user's PC/browser language automatically, allows manual language switching, and remembers the user's choice. The application console and the public website must share one visual language and one interaction model.

## Language Behavior

- Default language is resolved from stored preference first, then `navigator.languages`, then English.
- Manual switch supports English and Simplified Chinese.
- Manual choice is saved locally and reused on later visits.
- The console and website both show a compact language switch in persistent chrome.

## Visual Direction

- Positioning: bastion for AI services.
- Mood: dark sci-fi command center, calm, precise, operational, and visibly technical.
- Layout: dense but breathable, no marketing card overload inside the app.
- Palette: near-black space grid, high-contrast white text, cyan telemetry, emerald allowed-state, amber warning, red denied-state, restrained violet depth.
- Shape language: 8px radius, thin neon borders, glass panels only where they frame real tools.
- Atmosphere: scan lines, HUD rails, access topology rings, policy stream rows, terminal-like command blocks, no decorative blobs.
- Typography: system sans, no viewport-scaled text except landing hero.
- Data style: command chips, policy badges, target tags, audit rows, compact forms.

## App Design Pages

1. Auth
   - One auth form at a time.
   - Register/Login segmented switch.
   - DingTalk entry remains below the form.
   - Language switch visible before login.
2. Dashboard
   - Metrics, current org context, fast path actions, recent command decisions.
3. Organizations
   - Create, join, organization list, active scope cues.
4. Members and Groups
   - Add member, default all-members group, custom groups, member role table.
5. Public Keys
   - Add key form, fingerprint table, empty state.
6. SSH Services
   - Add direct/agent service, tags, filter chips, rename form.
7. Agent Enrollment
   - Create enrollment, run-once commands, service install commands with systemctl/sc.exe.
8. Command Policies
   - Policy creation, rule/binding forms, LLM configs, prompt resources.
9. Command Audit
   - Searchable decision rows, allow/deny badges, timestamps.
10. System Admin
   - DingTalk and LDAP global settings, users, organizations, member repair.

## Website Design Pages

1. Home EN
   - Hero names the product, sci-fi full-viewport scene, visible next section hint.
   - Language switch.
   - Architecture, safety posture, quickstart.
2. Home ZH
   - Same layout and visual rhythm with Chinese copy.
3. Docs EN
   - Documentation shell, side nav, quickstart, identity, targets, agents, policies, audit.
4. Docs ZH
   - Same layout and visual rhythm with Chinese copy.

## Design Output

Generate PNG design drafts under `design/mockups/output/`:

- `app-auth.png`
- `app-dashboard.png`
- `app-organizations.png`
- `app-members.png`
- `app-keys.png`
- `app-targets.png`
- `app-agents.png`
- `app-policies.png`
- `app-audit.png`
- `app-system-admin.png`
- `site-home-en.png`
- `site-home-zh.png`
- `site-docs-en.png`
- `site-docs-zh.png`
