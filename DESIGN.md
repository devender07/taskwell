---
name: Linear
colors:
  primary: "#62666D"
  secondary: "#08090A"
  surface: "#000000"
  on-surface: "#FFFFFF"
typography:
  body-md:
    fontFamily: Inter Variable
    fontSize: 15px
    fontWeight: 400
rounded:
  md: 9999px
---

# Design System Inspired by Linear

## 1. Visual Theme & Atmosphere

Linear's design system embodies a modern, minimalist aesthetic purpose-built for technical teams and AI-era product development. The visual language is characterized by extreme clarity, purposeful restraint, and sophisticated dark-mode elegance. Every element serves function with refined typography and generous whitespace creating a calm, focused environment. The system prioritizes legibility and information hierarchy while maintaining a contemporary, premium feel that appeals to software engineers and product professionals. Dark backgrounds with carefully calibrated neutral tones reduce cognitive load, while strategic accent colors (vibrant pink and emerald green) punctuate key interactions. The design avoids decoration, instead relying on depth, spacing, and subtle shadows to create visual structure.

**Key Characteristics**

- Dark-first design with sophisticated neutral palette
- Minimal, functional aesthetic with no unnecessary ornamentation
- Premium typography using Inter Variable and Berkeley Mono
- Generous spacing and whitespace for clarity and focus
- Subtle shadow and opacity systems for depth
- Strategic use of neon accent colors for key interactions
- Optimized for developer and technical audiences
- AI-era design language emphasizing clarity and efficiency

## 2. Color Palette & Roles

### Primary

- **Primary Background** (`#08090A`): Core dark background for hero sections and high-impact areas, representing the brand's sophisticated foundation
- **Primary Text** (`#F7F8F8`): Main readable text on dark backgrounds, used across body copy, headings, and interface labels
- **Primary Text Emphasis** (`#FFFFFF`): Pure white for maximum contrast and prominence in critical UI elements

### Accent Colors

- **Accent Pink** (`#F91880`): Vibrant brand accent for call-to-action buttons, highlights, and interactive elements requiring user attention
- **Accent Green** (`#00BA7C`): Semantic success and positive action indicator, used for completion states and affirmative interactions

### Interactive

- **Interactive Primary** (`#5E6AD2`): Primary link and interactive component color, representing actionable items with medium prominence
- **Interactive Hover** (`rgba(255, 255, 255, 0.05)`): Subtle hover state background for buttons and interactive surfaces
- **Interactive Border** (`rgba(255, 255, 255, 0.08)`): Refined border color for input fields and contained components

### Neutral Scale

- **Neutral 100** (`#F7F8F8`): Lightest neutral, used for primary text and high contrast surfaces
- **Neutral 200** (`#E5E5E6`): Very light neutral for secondary text and subtle backgrounds
- **Neutral 300** (`#D0D6E0`): Light neutral for borders, dividers, and tertiary content
- **Neutral 400** (`#8A8F98`): Medium-light gray for secondary text and disabled states
- **Neutral 500** (`#62666D`): Mid-tone gray for labels, captions, and muted content
- **Neutral 600** (`#3E3E44`): Dark gray for secondary navigation and minimized UI
- **Neutral 700** (`#18191A`): Very dark gray for strong contrast backgrounds
- **Neutral 800** (`#0F1011`): Near-black for nested containers and layered surfaces
- **Neutral 900** (`#000000`): Pure black for absolute darkness and strong emphasis

### Surface & Borders

- **Surface Overlay Light** (`rgba(255, 255, 255, 0.02)`): Minimal overlay for input field backgrounds, creating subtle visual separation
- **Surface Overlay Medium** (`rgba(255, 255, 255, 0.03)`): Medium opacity overlay for glass-morphic effects on cards
- **Surface Overlay Strong** (`rgba(255, 255, 255, 0.05)`): Strong overlay for active interactive states and contained elements
- **Border Light** (`rgba(255, 255, 255, 0.04)`): Inset border for subtle internal divisions
- **Border Standard** (`rgba(255, 255, 255, 0.05)`): Default border for cards, inputs, and component boundaries
- **Border Inset** (`rgba(0, 0, 0, 0.0003)`): Near-invisible inset border for very subtle depth

### Shadow Colors

- **Shadow Dark** (`rgba(0, 0, 0, 0.1)`): Light shadow for subtle depth on interactive elements
- **Shadow Medium** (`rgba(0, 0, 0, 0.4)`): Medium shadow for navigation and floating components
- **Shadow Strong** (`rgba(0, 0, 0, 0.6)`): Deep shadow for layered UI and elevation emphasis

## 3. Typography Rules

### Font Family

**Primary Font:** Inter Variable  
Fallback stack: `Inter, -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif`

**Code Font:** Berkeley Mono  
Fallback stack: `"Berkeley Mono", "Monaco", "Courier New", monospace`

### Hierarchy

| Role | Font | Size | Weight | Line Height | Letter Spacing | Notes |
|------|------|------|--------|-------------|----------------|-------|
| **Display 1** | Inter Variable | `64px` | `510` | `64px` | `0px` | Hero headlines, maximum prominence |
| **Display 2** | Inter Variable | `72px` | `510` | `72px` | `0px` | Extra-large hero text for impact |
| **Display 3** | Inter Variable | `48px` | `510` | `48px` | `0px` | Large section headings |
| **Display 4** | Inter Variable | `48px` | `590` | `48px` | `0px` | Bold variant of display text |
| **Heading 1** | Inter Variable | `40px` | `510` | `44px` | `0px` | Primary page headings |
| **Heading 2** | Inter Variable | `24px` | `590` | `31.92px` | `0px` | Subsection headings |
| **Heading 3** | Inter Variable | `20px` | `590` | `26.6px` | `0px` | Section subheadings |
| **Heading 4** | Inter Variable | `16px` | `590` | `24px` | `0px` | Card and component titles |
| **Heading 5** | Inter Variable | `15px` | `510` | `24px` | `0px` | Secondary headings and labels |
| **Heading 6** | Inter Variable | `13px` | `510` | `19.5px` | `0px` | Small UI headings |
| **Body Large** | Inter Variable | `20px` | `510` | `26.6px` | `0px` | Large body copy for emphasis |
| **Body** | Inter Variable | `16px` | `400` | `24px` | `0px` | Default body text |
| **Body Medium** | Inter Variable | `15px` | `400` | `24px` | `0px` | Standard content text |
| **Body Small** | Inter Variable | `14px` | `400` | `21px` | `0px` | Secondary content and labels |
| **Link** | Inter Variable | `14px` | `510` | `21px` | `0px` | Hyperlinks and navigation items |
| **Button** | Inter Variable | `13px` | `400` | `19.5px` | `0px` | Interactive button text |
| **Button Medium** | Inter Variable | `13px` | `510` | `19.5px` | `0px` | Emphasized button states |
| **Caption** | Inter Variable | `13px` | `400` | `19.5px` | `0px` | Small utility text |
| **Input** | Inter Variable | `13.3333px` | `400` | `normal` | `0px` | Form field text |
| **Code** | Berkeley Mono | `14px` | `400` | `21px` | `0px` | Inline and block code |

### Principles

- **Consistent vertical rhythm:** Line heights maintain 4px baseline grid alignment
- **Weight stratification:** Weight values (400, 510, 590) create clear hierarchy without multiple fonts
- **Size steps:** Font sizes follow logical progression for readable scanning
- **Font pairing:** Inter Variable for UI/content, Berkeley Mono for technical elements
- **Accessibility:** Minimum `14px` for body text ensures readability; line heights `≥ 1.4x` font size
- **Responsive scaling:** Font sizes scale down on mobile breakpoints while maintaining hierarchy

## 4. Component Stylings

### Buttons

**Primary Button**
- Background: `rgba(255, 255, 255, 0.05)`
- Text Color: `#F7F8F8`
- Font Size: `13px`
- Font Weight: `510`
- Font Family: Inter Variable
- Padding: `0px 14px`
- Height: `40px`
- Border Radius: `9999px`
- Border: `0px none`
- Box Shadow: `rgba(255, 255, 255, 0.03) 0px 0px 0px 1px inset, rgba(255, 255, 255, 0.04) 0px 1px 0px 0px inset, rgba(0, 0, 0, 0.6) 0px 0px 0px 1px, rgba(0, 0, 0, 0.1) 0px 4px 4px 0px`
- Line Height: `40px`
- Hover: Increase opacity to `rgba(255, 255, 255, 0.08)`, maintain shadow

**Secondary Button**
- Background: `#E5E5E6`
- Text Color: `#08090A`
- Font Size: `13px`
- Font Weight: `510`
- Font Family: Inter Variable
- Padding: `0px 12px`
- Height: `32px`
- Border Radius: `9999px`
- Border: `1px solid #E5E5E6`
- Box Shadow: `rgba(0, 0, 0, 0) 0px 8px 2px 0px, rgba(0, 0, 0, 0.01) 0px 5px 2px 0px, rgba(0, 0, 0, 0.04) 0px 3px 2px 0px, rgba(0, 0, 0, 0.07) 0px 1px 1px 0px, rgba(0, 0, 0, 0.08) 0px 0px 1px 0px`
- Line Height: `32px`
- Hover: Reduce background opacity to `0.9`, enhance shadow slightly

**Ghost Button**
- Background: `rgba(0, 0, 0, 0)`
- Text Color: `#8A8F98`
- Font Size: `13px`
- Font Weight: `400`
- Font Family: Inter Variable
- Padding: `0px 12px`
- Height: `32px`
- Border Radius: `9999px`
- Border: `0px none`
- Box Shadow: `none`
- Line Height: `19.5px`
- Hover: Text color shifts to `#F7F8F8`, subtle background `rgba(255, 255, 255, 0.05)` appears

**Navigation Button**
- Background: `rgba(0, 0, 0, 0)`
- Text Color: `#F7F8F8`
- Font Size: `16px`
- Font Weight: `400`
- Font Family: Inter Variable
- Padding: `0px`
- Height: `72px`
- Border Radius: `0px`
- Border: `0px none`
- Box Shadow: `none`
- Line Height: `24px`
- Hover: Color shifts to `#8A8F98`, no background change

**Input Button (Search/Mini)**
- Background: `rgba(0, 0, 0, 0)`
- Text Color: `#FFFFFF`
- Font Size: `13.3333px`
- Font Weight: `400`
- Font Family: Inter Variable
- Padding: `0px 4px`
- Height: `28px`
- Border Radius: `4px`
- Border: `0px none`
- Box Shadow: `none`
- Line Height: `normal`
- Hover: Subtle background `rgba(255, 255, 255, 0.03)` emerges

### Cards & Containers

**Dark Card**
- Background: `#0F1011`
- Text Color: `#F7F8F8`
- Font Size: `16px`
- Font Weight: `400`
- Font Family: Inter Variable
- Padding: `0px 24px 28px 24px`
- Border Radius: `8px`
- Border: `1px solid rgba(255, 255, 255, 0.05)`
- Box Shadow: `none`
- Line Height: `24px`
- Hover: Border opacity increases to `rgba(255, 255, 255, 0.08)`

**Content Card**
- Background: `rgba(0, 0, 0, 0)`
- Text Color: `#F7F8F8`
- Font Size: `16px`
- Font Weight: `400`
- Font Family: Inter Variable
- Padding: `0px 32px 0px 32px` or `32px 0px 0px 0px` depending on context
- Border Radius: `0px`
- Border: `0px none`
- Box Shadow: `none`
- Line Height: `24px`

**Image Card**
- Background: `rgba(0, 0, 0, 0)` (transparent, contains media)
- Text Color: `#F7F8F8`
- Font Size: `16px`
- Font Weight: `400`
- Font Family: Inter Variable
- Padding: `0px`
- Border Radius: `0px`
- Border: `0px none`
- Box Shadow: `none`
- Line Height: `24px`

### Inputs & Forms

**Text Input Dark**
- Background: `rgba(255, 255, 255, 0.02)`
- Text Color: `#D0D6E0`
- Font Size: `13.3333px`
- Font Weight: `400`
- Font Family: Inter Variable
- Padding: `12px 14px`
- Height: `68px`
- Border Radius: `6px`
- Border: `1px solid rgba(255, 255, 255, 0.08)`
- Box Shadow: `rgba(0, 0, 0, 0.2) 0px 0px 0px 1px`
- Line Height: `normal`
- Placeholder: Color `#8A8F98` at `0.6` opacity
- Focus: Border color to `rgba(255, 255, 255, 0.15)`, background to `rgba(255, 255, 255, 0.04)`

**Text Input Light**
- Background: `rgba(255, 255, 255, 0.05)`
- Text Color: `#FFFFFF`
- Font Size: `14px`
- Font Weight: `400`
- Font Family: Inter Variable
- Padding: `0px 10px`
- Height: `40px`
- Border Radius: `8px`
- Border: `1px solid rgba(255, 255, 255, 0.05)`
- Box Shadow: `none`
- Line Height: `21px`
- Focus: Border opacity to `rgba(255, 255, 255, 0.1)`, background to `rgba(255, 255, 255, 0.08)`

**Search Input Large**
- Background: `rgba(0, 0, 0, 0)`
- Text Color: `#F7F8F8`
- Font Size: `16px`
- Font Weight: `400`
- Font Family: Inter Variable
- Padding: `1px 32px`
- Height: `64px`
- Border Radius: `0px`
- Border: `0px none`
- Box Shadow: `none`
- Line Height: `normal`
- Cursor: Auto on focus

**Code Input**
- Background: `rgba(0, 0, 0, 0)`
- Text Color: `rgba(0, 0, 0, 0)` (monospace rendering)
- Font Size: `14px`
- Font Weight: `400`
- Font Family: Berkeley Mono
- Padding: `0px 32px 0px 55.8px`
- Height: `432px`
- Border Radius: `0px`
- Border: `0px none`
- Box Shadow: `none`
- Line Height: `24px`

### Navigation

**Header Navigation**
- Background: `rgba(0, 0, 0, 0)`
- Text Color: `#F7F8F8`
- Font Size: `16px`
- Font Weight: `400`
- Font Family: Inter Variable
- Padding: `0px`
- Height: `72px`
- Border Radius: `0px`
- Border: `0px none`
- Box Shadow: `none`
- Line Height: `24px`
- Active Item: Color to `#FFFFFF`, opacity `1.0`
- Inactive Item: Color to `#8A8F98`, opacity `0.7`

### Links

**Primary Link**
- Background: `#5E6AD2`
- Text Color: `#FFFFFF`
- Font Size: `14px`
- Font Weight: `510`
- Font Family: Inter Variable
- Padding: `0px 16px`
- Height: `32px`
- Border Radius: `0px`
- Border: `0px none`
- Box Shadow: `none`
- Line Height: `21px`
- Hover: Background opacity to `0.8`

**Secondary Link**
- Background: `rgba(0, 0, 0, 0)`
- Text Color: `#F7F8F8`
- Font Size: `16px`
- Font Weight: `400`
- Font Family: Inter Variable
- Padding: `0px 8px`
- Height: `32px`
- Border Radius: `6px`
- Border: `0px none`
- Box Shadow: `none`
- Line Height: `24px`
- Hover: Background to `rgba(255, 255, 255, 0.05)`

**Tertiary Link**
- Background: `rgba(0, 0, 0, 0)`
- Text Color: `#8A8F98`
- Font Size: `13px`
- Font Weight: `400`
- Font Family: Inter Variable
- Padding: `0px 12px`
- Height: `32px`
- Border Radius: `9999px`
- Border: `0px none`
- Box Shadow: `none`
- Line Height: `19.5px`
- Hover: Color to `#F7F8F8`, background to `rgba(255, 255, 255, 0.05)`

## 5. Layout Principles

### Spacing System

**Base Unit:** `8px`

**Spacing Scale:**
- `4px`: Tight micro-spacing for elements within components
- `8px`: Gap between adjacent related elements
- `12px`: Padding within compact components
- `16px`: Standard padding for inputs and buttons
- `20px`: Margin between related content blocks
- `24px`: Default padding for cards and containers
- `32px`: Generous padding for primary sections
- `40px`: Gap between major content sections
- `48px`: Padding for hero sections and stage areas
- `56px`: Large breathing room for section separation
- `96px`: Maximum top/bottom margin for whitespace emphasis

**Usage Context:**
- Use `4px–8px` for button/icon spacing
- Use `12px–16px` for form fields and compact UI
- Use `24px–32px` for cards and content containers
- Use `40px–96px` for section separation and hero areas

### Grid & Container

**Max Width:** `1440px` (standard desktop container)

**Column Strategy:** 12-column responsive grid with flexible alignment

**Section Patterns:**
- Full-width hero sections: `0px` horizontal padding at viewport edges
- Content sections: Center container with `32px–48px` padding on sides (responsive)
- Card grids: 3-column layout on desktop, 2-column on tablet, 1-column on mobile with `24px` gap
- Navigation: Fixed-width `1440px` header spanning full viewport

### Whitespace Philosophy

Linear emphasizes generous whitespace and breathing room to reduce cognitive load and enhance focus. Sections are separated by substantial margins (`40px–96px`), and cards/components have adequate padding (`24px–32px`) to prevent visual crowding. The dark background amplifies whitespace impact, making negative space itself a design element. Content never feels rushed; instead, hierarchical spacing guides the eye and establishes reading rhythm. Whitespace is used strategically to emphasize importance—larger gaps highlight more significant transitions.

### Border Radius Scale

- `0px`: Full-width hero sections, navigation, large container backgrounds, code blocks (sharp edge, maximum space efficiency)
- `4px`: Small icon buttons, tight form controls, utility buttons
- `6px`: Input fields, compact cards, standard form elements
- `8px`: Medium cards, content containers, component groups
- `9999px`: Fully rounded pill buttons, avatar circles, badges, micro-interactions

### Border Widths

- **Thin (1px):** Default border for inputs, cards, and component boundaries; used for subtle visual separation
- **Light inset (1px internal):** Inset borders for glass-morphic effects and internal component divisions

## 6. Depth & Elevation

| Level | Treatment | Use |
|-------|-----------|-----|
| **Base** (z-index 1) | No shadow, flat surface | Background layers, neutral content |
| **Raised** (z-index 2–3) | `rgba(0, 0, 0, 0.1) 0px 4px 4px 0px` | Standard cards, component surfaces |
| **Floating** (z-index 50) | `rgba(0, 0, 0, 0.4) 0px 1px 0px 0px` | Dropdowns, tooltips, modals |
| **Sticky** (z-index 995–997) | `rgba(0, 0, 0, 0.4) 0px 1px 0px 0px` | Fixed navigation, persistent overlays |
| **Modal** (z-index 100+) | `rgba(0, 0, 0, 0.6) 0px 0px 0px 1px, rgba(0, 0, 0, 0.1) 0px 4px 4px 0px` | Dialogs, alerts, fullscreen overlays |

**Shadow Philosophy:**

Linear uses a restrained shadow system that reinforces depth without creating visual clutter. Shadows are subtle and dark (leveraging the dark background), appearing only where layering is necessary. The system employs inset shadows for internal component divisions and outset shadows for elevation. Combined box-shadow declarations create sophisticated depth: inset borders provide subtle internal definition, while outset shadows establish layering hierarchy. This creates a modern, refined aesthetic that feels premium and technical.

### Opacity Levels

- `0.30` (30%): Extremely subtle overlays, disabled states on hover
- `0.40` (40%): Muted secondary text, disabled interactive elements
- `0.60` (60%): Placeholder text, inactive navigation items
- `0.70` (70%): Secondary text, reduced prominence
- `0.80` (80%): Tertiary text, caption content
- `0.90` (90%): Near-full opacity for slightly reduced emphasis

### Z-index / Layering

- **Base:** `1` (default content layer)
- **Raised:** `2–3` (cards, components slightly above base)
- **Dropdown:** `50` (dropdown menus, context menus)
- **Sticky:** `995–997` (fixed navigation, persistent headers)
- **Modal:** `100+` (dialogs, fullscreen overlays, alerts)

This layering strategy ensures predictable stacking behavior while reserving headroom for future components.

## 7. Do's and Don'ts

### Do

- **Use the full 8px grid spacing system** consistently; all padding, margin, and gaps should be multiples of 8px for visual harmony
- **Prioritize whitespace:** Allow generous breathing room between sections; empty space is a design element, not wasted space
- **Maintain typography hierarchy:** Use the specified font sizes and weights to create clear visual hierarchy; never skip hierarchy levels
- **Apply shadows sparingly:** Use shadows only for layering; rely on color, borders, and spacing for primary visual structure
- **Emphasize with accent colors:** Use `#F91880` (pink) and `#00BA7C` (green) strategically for CTAs, alerts, and critical interactions
- **Test accessibility:** Ensure minimum contrast ratios (4.5:1 for text); use weight variation alongside color for non-color-dependent meaning
- **Scale responsively:** Reduce font sizes, padding, and gaps on mobile while maintaining hierarchy
- **Use border radius purposefully:** Pill buttons (`9999px`) for primary actions, moderate radii (`6px–8px`) for containers, zero radius for full-width sections
- **Apply consistent opacity levels:** Use the established opacity scale for disabled states, secondary text, and overlays
- **Nest components intentionally:** Use the z-index scale (`1, 2, 50, 995, 100`) to avoid stacking conflicts

### Don't

- **Don't mix spacing systems:** Avoid arbitrary padding/margin values; always use the 8px scale or justify deviations explicitly
- **Don't overuse shadows:** Shadows should enhance layering only; never apply multiple overlapping shadows unless intentional for depth
- **Don't ignore color contrast:** Low contrast combinations (e.g., `#62666D` on `#0F1011`) are accessible for secondary text only; never use for primary content
- **Don't apply rounded corners carelessly:** Reserve high border radius values (`9999px`) for buttons; use lower values (`0–8px`) for containers to maintain visual weight
- **Don't create custom font sizes:** Stick to the established typography hierarchy; custom sizes fragment the system and reduce recognition
- **Don't hardcode z-index values arbitrarily:** Always reference the layering scale (`1, 2, 50, 995, 100`); prevent stacking wars and conflicts
- **Don't forget line-height:** Maintain minimum line-height of `1.4x` font size; poor line-height destroys readability on dark backgrounds
- **Don't over-saturate with accent colors:** Limit pink (`#F91880`) and green (`#00BA7C`) to primary actions and semantic states; excessive use dilutes impact
- **Don't skip responsive design:** Mobile layouts must scale spacing down (`16px` sections instead of `32px`) while maintaining hierarchy
- **Don't rely on color alone:** Always combine color with shape, weight, or positioning for meaning (e.g., disabled state should use opacity + desaturation, not color alone)

## 8. Responsive Behavior

### Breakpoints

| Name | Width | Key Changes |
|------|-------|-------------|
| **Mobile** | `320px–639px` | Single-column layout, `16px` section padding, `12px` component spacing, font sizes reduced by 2–4px, stacked navigation |
| **Tablet** | `640px–1023px` | 2-column grid, `24px` section padding, `16px` component spacing, maintained font hierarchy, collapsible sidebar |
| **Desktop** | `1024px–1439px` | 3-column grid, `32px` section padding, full horizontal navigation, optimal line lengths (60–75 characters) |
| **Large Desktop** | `1440px+` | Max-width container `1440px`, centered with gutters, 4-column grid option, expansive whitespace |

### Touch Targets

- **Minimum Interactive Size:** `44px × 44px` (buttons, links, tap targets)
- **Comfortable Button Height:** `40px` (standard mobile tap area)
- **Compact Button Height:** `32px` (acceptable for desktop, increase padding on mobile)
- **Icon Button Size:** `40px × 40px` (with internal icon `20px × 20px`)
- **Spacing Between Touch Targets:** Minimum `8px` gap; recommend `12px` to prevent accidental taps

### Collapsing Strategy

- **Navigation:** Desktop horizontal nav collapses to hamburger menu on tablet; full sidebar on mobile
- **Grid Layouts:** 3-column → 2-column at `1024px` → 1-column at `640px`; adjust card width to fill containers
- **Spacing:** Reduce section margins (`40px → 32px → 24px`) and padding (`32px → 24px → 16px`) at smaller breakpoints
- **Typography:** Scale display sizes down by 20–25% on tablet; further reduce on mobile while maintaining hierarchy (e.g., `64px` Display → `48px` Tablet → `32px` Mobile)
- **Images:** Scale from 100% to 50% width on tablet; center-crop on mobile if needed
- **Containers:** Remove max-width constraints at mobile; use full viewport with safe `16px` gutters
- **Modals/Overlays:** Fullscreen on mobile; constrained to `90vw` on tablet; fixed size on desktop

## 9. Agent Prompt Guide

### Quick Color Reference

- **Primary CTA:** Primary Button (`rgba(255, 255, 255, 0.05)` background, `#F7F8F8` text)
- **Accent Action:** Accent Pink (`#F91880`) for highlights, warnings, or high-impact actions
- **Success/Positive:** Accent Green (`#00BA7C`) for completion, checkmarks, affirmative states
- **Background (Hero):** Primary Background (`#08090A`)
- **Background (Nested):** Primary Nested (`#0F1011`)
- **Heading Text:** Primary Text (`#F7F8F8`) or emphasis (`#FFFFFF`)
- **Body Text:** Primary Text (`#F7F8F8`) or secondary (`#8A8F98`)
- **Borders:** Border Standard (`rgba(255, 255, 255, 0.05)`)
- **Input Field:** Surface Overlay Light (`rgba(255, 255, 255, 0.02)`)
- **Disabled/Muted:** Neutral 400 (`#8A8F98`)

### Iteration Guide

1. **Establish the grid:** All spacing must conform to the `8px` scale; no arbitrary padding or margin values. Use `8px, 12px, 16px, 24px, 32px, 40px, 48px, 56px, 96px` only.

2. **Set typography hierarchy:** Apply exact font sizes and weights from the Typography Hierarchy table. Never create custom sizes; hierarchy must be instantly recognizable. Use `Inter Variable` for all UI text and `Berkeley Mono` for code.

3. **Color consistently:** Use the semantic color roles (Primary, Accent, Interactive, Neutral). Maintain high contrast on dark backgrounds; verify WCAG AA compliance (`4.5:1` for text). Limit accent colors to high-priority interactions.

4. **Apply shadows purposefully:** Use the Elevation table's shadow values only. Base level (`0px` shadow), Raised (`rgba(0, 0, 0, 0.1) 0px 4px 4px 0px`), Floating/Sticky (`rgba(0, 0, 0, 0.4) 0px 1px 0px 0px`), Modal/Critical (`combined inset + outset`).

5. **Implement component variants:** Button, Input, Card, Link, Navigation styles MUST match the Component Stylings section exactly—including padding, radius, border, shadow, font. No deviations without justification.

6. **Scale responsively:** Apply Breakpoint rules at `640px`, `1024px`, `1440px`. Reduce font sizes `2–4px` on mobile, adjust padding `32px → 24px → 16px`, change grid from 3-column → 2-column → 1-column.

7. **Layer intentionally:** Use the Z-index / Layering scale strictly (`1` base, `2–3` raised, `50` dropdown, `995–997` sticky, `100+` modal). This prevents stacking conflicts and ensures predictable depth.

8. **Respect whitespace:** Sections MUST have `40px–96px` separation; components `24px–32px` padding. Dark background amplifies negative space; generous whitespace is a core brand attribute, not waste.

9. **Verify focus & hover states:** Every interactive element requires a focus state (visible indicator for keyboard navigation) and hover state (opacity, color, or shadow change). Test with keyboard and screen readers.

10. **Validate accessibility:** Minimum touch target `44px × 44px`, minimum line-height `1.4x` font size, minimum contrast `4.5:1` for text. Use opacity + shape/weight for meaning, never color alone.