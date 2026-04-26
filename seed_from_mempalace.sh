#!/bin/bash
# Seeds memoryweb from mempalace content + git log context
# Run with DB pointing at the live DB
BINARY=/Users/mattcorby-eaglen/.memoryweb/memoryweb
DB=/Users/mattcorby-eaglen/.memoryweb/.memoryweb.db
export MEMORYWEB_DB=$DB

call() {
  echo "$1" | $BINARY
}

add_node() {
  local label="$1" desc="$2" why="$3" domain="$4"
  call "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"add_node\",\"arguments\":{\"label\":$(printf '%s' "$label" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))'),\"description\":$(printf '%s' "$desc" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))'),\"why_matters\":$(printf '%s' "$why" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))'),\"domain\":$(printf '%s' "$domain" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))') }}}"
}

add_edge() {
  local from="$1" to="$2" rel="$3" narrative="$4"
  call "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"add_edge\",\"arguments\":{\"from_node\":$(printf '%s' "$from" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))'),\"to_node\":$(printf '%s' "$to" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))'),\"relationship\":$(printf '%s' "$rel" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))'),\"narrative\":$(printf '%s' "$narrative" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))') }}}"
}

echo "=== Seeding deep-game nodes ==="

add_node \
  "CRT_ORG_BANK fix — Deep runs" \
  "z88dk newlib CRT defaults CRT_ORG_BANK_N=0xC000 (16-bit). The banked call trampoline reads byte[2] as the 8K page number — 16-bit values have byte[2]=0, so page 0 (ROM) gets mapped into \$C000 and the CPU crashes. Fix: override all 8 CRT_ORG_BANK values in zpragma.inc with 32-bit values encoding the 8K page: CRT_ORG_BANK_32=0x40C000 ... CRT_ORG_BANK_39=0x4EC000. Formula: CRT_ORG_BANK_N = ((N*2) << 16) | 0xC000. Binary verified: 234 banked calls across 9 NEX blocks, all with correct non-zero page bytes." \
  "This was the single root cause that prevented Deep from running at all. Every banked call jumped into ROM. First filed as undocumented z88dk behaviour — no existing ZXN newlib banked example existed anywhere." \
  "deep-game"

add_node \
  "REGISTER_SP slot-7 stack wipe" \
  "REGISTER_SP=0xFFFF placed the main stack inside slot 7 (0xE000-0xFFFF). banked_call remaps slots 6+7 on every __banked call. The very first banked call wiped the stack — return address corrupted, reset to 48K BASIC. Fix: REGISTER_SP=0xBFFF moves stack to slot 5 (0xA000-0xBFFF), which banked_call never remaps. Slot layout: 0/1=ROM, 2/3=ZXNext system, 4/5=code+BSS+stack, 6/7=banked region." \
  "Identified during April 2026 banking investigation. Was investigated before the CRT_ORG_BANK fix — both bugs existed simultaneously, masking each other." \
  "deep-game"

add_node \
  "BSS overflow — DeepGame struct split" \
  "BSS tail was at \$C719 (1,817 bytes past \$C000). BSS zero-init on startup trashed the banked window, crashing before main() ran. Root cause: two 64x64 zone buffers inside DeepGame (8,192 bytes total). Fix: extracted zone[] and zone_preload[] into BSS globals, reduced _deep_ula from 32 to 4 rows (saves 1,148B), extracted play/ula/messages arrays. Final BSS tail: \$BD98 — 616 bytes clear of \$C000. _game struct shrank from 11,759 to 306 bytes." \
  "BSS overflowing past \$C000 is silent and catastrophic — the banked window starts there and any BSS init wipes the bank page table. Must keep BSS tail below \$C000 at all times." \
  "deep-game"

add_node \
  "NextOS IM1 ISR green stripes bug" \
  "Symptom: green vertical stripes covering ~75% of screen, crashes on any keypress. Root cause: NextOS IM1 ISR at \$0038 fires every VBLANK and rewrites Copper RAM, restoring the NextOS Copper program (WAIT line=32, MOVE \$69=0x40) which re-enables LoRes mode at scanline 32. \$0038 is in ROM in a NEX context — C pointer writes are silently ignored. ZRCP confirmed: read-memory 0x0038 \u2192 F3 AF 11 FF (NextOS DI+handler). Approaches that failed: writing \$69=0x00 in game loop, Copper rewrite, intrinsic_di()+RETI stub at \$0038." \
  "Display was completely broken even after banking and BSS were fixed. Required understanding the ZX Next interrupt architecture and NextOS ownership of Copper RAM." \
  "deep-game"

add_node \
  "IM2 IRQ handler + CFrame keyboard" \
  "Fix for NextOS ISR. IM2 vector table: 257 bytes at \$8100-\$8200 (I=\$81), all entries pointing to \$81FF:\$8200. ULA drives \$FF on bus during interrupt acknowledge so Z80 reads vector from [\$81FF:\$8200]. ISR: push af, ld a,1, ld (_g_vblank),a, pop af, reti. Copper program now writes MOVE \$69=0 + HALT every VBLANK in hardware — permanently guarantees \$69=0 before any scanline renders. Keyboard: inline Z80 ASM using IN A,(C), 8-row scan into g_keys[40]. CFrame (Mike Dailly, original Lemmings) extracted and adapted for Deep. BSS tail: \$BFEF, 17 bytes clear." \
  "Resolved all remaining display and input issues. IM2 handler owns the interrupt, Copper program guarantees the display register state, keyboard scan works without z88dk input library." \
  "deep-game"

add_node \
  "memcpy ABI crash — sccz80 vs sdcc_ix" \
  "Root cause: z88dk's sdcc_ix variants of _memcpy_callee/_memset_callee pop arguments in the opposite order from what sccz80 pushes at call sites. So memcpy(dst, src, 11) ran LDIR with bc=dst (~32KB count) and de=11 (destination), wiping huge swaths of RAM. MMU drifted, slots 6/7 remapped, system fell to BASIC ROM. cls() survived only because most garbage writes landed in ROM and were ignored. Fix: ensure consistent ABI throughout — don't mix sdcc_ix and sccz80 code." \
  "Silent data corruption that presented as a random-looking crash. Especially dangerous because cls() appeared to work, masking the underlying RAM state corruption." \
  "deep-game"

add_node \
  "DeZog + ZEsarUX debug setup" \
  "DeZog VSCode extension + ZEsarUX emulator with ZRCP on port 10200. SLD file generated by z88dk build. Allows source-level debugging of ZX Next code. BSS boundary check added to check_banks.sh. Bank headroom as of April 2026: BANK_32=15,700B free, BANK_34=10,618B, BANK_35=7,260B, BANK_36=11,948B, BANK_37=13,179B, BANK_38=15,185B, BANK_39=757B (WATCH — tight)." \
  "Required for diagnosing crashes that are invisible at the emulator level. ZRCP memory reads confirmed ISR state, page mappings, and BSS layout during debugging sessions." \
  "deep-game"

add_node \
  "Demo sprint structure" \
  "Sprint plan targeting a demonstrable build. Stories grouped into: headless core (game logic without display), splash loader (title screen, boot sequence), graphics layer (ULA/sprite rendering), and SD asset loading. STORY-085/086: DeepLayerReport struct implemented. STORY-108: deep_layer_report.c excluded from ZX Next build until ready. Acceptance: emulator screenshot + approval gate on stories." \
  "Defines the critical path for getting something showable. All the banking, BSS, display, and input fixes feed into this sprint." \
  "deep-game"

echo ""
echo "=== Seeding sedex nodes ==="

add_node \
  "TNOVA-182 own-site workplace SAQ filter" \
  "Adds own-site workplace filtering (siteType: internal/external) to the visibility service. WorkplaceApi.getWorkplacesByOrgCodes as source of own-site data. Feature toggles own-site workplaces via features API. Branch: TNOVA-182-own-site-saq-filter. Status: code complete, pending v3-flag integration tests. Note: siteType values changed from 'ownsite/supplier' to 'internal/external' during implementation." \
  "Core SAQ filter feature for Terra Nova. Code is complete but blocked on integration tests and the feature flag story TNOVA-182-feature-flag." \
  "sedex"

add_node \
  "TNOVA-182-B outside-in test conversion" \
  "Child story of TNOVA-182. public-api-cache-loader needs outside-in test conversion. Source of own-site data: WorkplaceApi.getWorkplacesByOrgCodes." \
  "Test coverage gap that needs addressing before TNOVA-182 can be considered fully done." \
  "sedex"

add_node \
  "TNOVA-182 feature flag (blocked by TNOVA-228)" \
  "Feature flag story for TNOVA-182. Currently blocked by TNOVA-228." \
  "Cannot ship the own-site filter behind a flag until TNOVA-228 is resolved." \
  "sedex"

add_node \
  "TNOVA-228" \
  "Blocks the TNOVA-182 feature flag story. Details not fully captured — check Jira." \
  "Critical blocker for the TNOVA-182 feature flag path." \
  "sedex"

add_node \
  "scoring-inherent-risk-model" \
  "Scoring model repo at ~/repos/scoring-inherent-risk-model. PRs merged: okhttp 4→5 (PR7), http4k 5→6 (PR14), gradle wrapper 9.0→9.4.1 (PR16), kotlinx-coroutines 1.7.3→1.10.2 (PR15), apache-poi 5.4→5.5.1 (PR13), commons-lang3 3.18→3.20 (PR12), rhino 1.7.14.1→1.9.1 (PR11), guava 32→33.6 (PR10), download-artifact v4→v8 (PR9), dataframe-excel 0.12→0.15 (PR8). Service ownership identified by serviceOwner in configuration class." \
  "Dependency maintenance PRs all merged April 2026. Both major breaking upgrades (okhttp 4→5, http4k 5→6) landed successfully." \
  "sedex"

add_node \
  "story-078" \
  "All 4 acceptance criteria implemented, tests passing, pending commit as of 2026-04-15." \
  "Nearly done — just needs the commit." \
  "sedex"

add_node \
  "binder service" \
  "Main Sedex API service. Local repo: ~/repos/binder. Active branch: TNOVA-182-own-site-saq-filter. Service ownership identified by serviceOwner in configuration class. Inherent Risk Pipeline Confluence page ID: 5789384708, parent: Terra Nova Dev Space 5285085200, version: 5." \
  "Core service where TNOVA-182 work is happening. Binder is the entry point for most Sedex API features." \
  "sedex"

echo ""
echo "=== Seeding edges ==="

# deep-game edges

# Get node IDs for edge creation - we need the slug-based IDs
CRT_FIX=$(sqlite3 $DB "SELECT id FROM nodes WHERE label LIKE 'CRT_ORG_BANK%' LIMIT 1;")
SP_FIX=$(sqlite3 $DB "SELECT id FROM nodes WHERE label LIKE 'REGISTER_SP%' LIMIT 1;")
BSS_FIX=$(sqlite3 $DB "SELECT id FROM nodes WHERE label LIKE 'BSS overflow%' LIMIT 1;")
ISR_BUG=$(sqlite3 $DB "SELECT id FROM nodes WHERE label LIKE 'NextOS IM1%' LIMIT 1;")
IM2_FIX=$(sqlite3 $DB "SELECT id FROM nodes WHERE label LIKE 'IM2 IRQ%' LIMIT 1;")
MEMCPY=$(sqlite3 $DB "SELECT id FROM nodes WHERE label LIKE 'memcpy ABI%' LIMIT 1;")
DEZOG=$(sqlite3 $DB "SELECT id FROM nodes WHERE label LIKE 'DeZog%' LIMIT 1;")
DEMO_SPRINT=$(sqlite3 $DB "SELECT id FROM nodes WHERE label LIKE 'Demo sprint%' LIMIT 1;")

RST10=$(sqlite3 $DB "SELECT id FROM nodes WHERE label LIKE 'RST%10%' LIMIT 1;")
STRAITJACKET=$(sqlite3 $DB "SELECT id FROM nodes WHERE label LIKE 'Straitjacket%' LIMIT 1;")
Z88DK=$(sqlite3 $DB "SELECT id FROM nodes WHERE label LIKE 'z88dk%' LIMIT 1;")

echo "CRT_FIX=$CRT_FIX SP_FIX=$SP_FIX BSS_FIX=$BSS_FIX ISR_BUG=$ISR_BUG IM2_FIX=$IM2_FIX"

add_edge "$CRT_FIX" "$RST10" "unblocks" \
  "The CRT_ORG_BANK fix is what actually got the game loading for the first time — without it every banked call jumped into ROM. RST \$10 is the next layer of crash that appears once the binary is actually running."

add_edge "$SP_FIX" "$CRT_FIX" "led_to" \
  "Both bugs were investigated together in April 2026. The SP investigation revealed the slot layout understanding that later clarified why the page encoding in CRT_ORG_BANK mattered."

add_edge "$BSS_FIX" "$ISR_BUG" "unblocks" \
  "Only after BSS was cleared below \$C000 and the game was actually running could the display layer bug (green stripes) be diagnosed — the crashes before BSS fix masked it."

add_edge "$IM2_FIX" "$ISR_BUG" "unblocks" \
  "IM2 IRQ handler takes ownership of the interrupt vector from NextOS, stopping the ISR from firing and restoring Copper RAM each VBLANK. That's what ends the green stripes."

add_edge "$MEMCPY" "$BSS_FIX" "led_to" \
  "The memcpy ABI crash was discovered after the BSS fix — it was masked while the BSS overflow was also trashing RAM. Fixing BSS revealed the next layer of corruption."

add_edge "$DEMO_SPRINT" "$IM2_FIX" "depends_on" \
  "The demo sprint can't produce a showable build without display and input working — IM2 IRQ handler and Copper fix are prerequisites for the graphics layer stories."

add_edge "$CRT_FIX" "$Z88DK" "is_example_of" \
  "The CRT_ORG_BANK fix is a direct consequence of undocumented z88dk newlib behaviour — it's the clearest example of why deep understanding of the toolchain is critical on this target."

# sedex edges
TNOVA182=$(sqlite3 $DB "SELECT id FROM nodes WHERE label LIKE 'TNOVA-182 own-site%' LIMIT 1;")
TNOVA182B=$(sqlite3 $DB "SELECT id FROM nodes WHERE label LIKE 'TNOVA-182-B%' LIMIT 1;")
TNOVA182FF=$(sqlite3 $DB "SELECT id FROM nodes WHERE label LIKE 'TNOVA-182 feature%' LIMIT 1;")
TNOVA228=$(sqlite3 $DB "SELECT id FROM nodes WHERE label LIKE 'TNOVA-228%' LIMIT 1;")
BINDER=$(sqlite3 $DB "SELECT id FROM nodes WHERE label LIKE 'binder service%' LIMIT 1;")

add_edge "$TNOVA182" "$TNOVA182B" "has_child_story" \
  "TNOVA-182-B is the outside-in test conversion work that needs to happen alongside the main feature implementation."

add_edge "$TNOVA182FF" "$TNOVA228" "blocked_by" \
  "Can't ship TNOVA-182 behind a feature flag until TNOVA-228 is resolved."

add_edge "$TNOVA182" "$BINDER" "depends_on" \
  "TNOVA-182 own-site filter is implemented in the binder service — WorkplaceApi.getWorkplacesByOrgCodes, visibility service, features API integration."

echo ""
echo "=== Done. Final node/edge counts ==="
sqlite3 $DB "SELECT COUNT(*) || ' nodes' FROM nodes; SELECT COUNT(*) || ' edges' FROM edges;"

