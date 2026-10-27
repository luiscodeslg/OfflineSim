#!/usr/bin/env python3
"""Stand-in for simc used only to exercise OfflineSim's plumbing in CI/dev.
It mimics simc's CLI surface (banner, progress bars, json2/html/save outputs)
with made-up numbers. It is NOT a simulator."""
import json, os, random, re, sys, time
BANNER = os.environ.get("FAKE_SIMC_BANNER", "SimulationCraft 1200-01 for World of Warcraft 12.0.5.99999 Live (hotfix 2026-10-20/99999, git build midnight abc1234)")
if len(sys.argv) < 2:
    print(BANNER); print("Usage: simc [options]"); sys.exit(0)
opts, players, profilesets = {}, [], {}
for raw in open(sys.argv[1], encoding="utf-8"):
    l = raw.strip()
    if not l or l.startswith("#"): continue
    m = re.match(r'^profileset\."([^"]+)"\+?=(.*)$', l)
    if m: profilesets.setdefault(m.group(1), []).append(m.group(2)); continue
    k, _, v = l.partition("=")
    if k in ("deathknight","demonhunter","druid","evoker","hunter","mage","monk","paladin","priest","rogue","shaman","warlock","warrior"):
        players.append({"name": v.strip('"'), "cls": k, "spec": "", "actions": []}); continue
    if k == "copy":
        players.append({"name": v.split(",")[0], "cls": players[-1]["cls"], "spec": players[-1]["spec"], "actions": []}); continue
    if k == "spec" and players: players[-1]["spec"] = v
    if k.startswith("actions") and players: players[-1]["actions"].append(l)
    if k == "bad_option": print("Error: unknown option 'bad_option'"); sys.exit(1)
    opts[k.rstrip("+")] = v
print(BANNER)
print("\nGenerating baseline...")
n = 50
for i in range(1, n + 1):
    bar = "=" * (i * 20 // n) + ">" + "." * (20 - i * 20 // n)
    sys.stdout.write(f"\rGenerating Baseline: {players[0]['name']} [{bar}] {i*200}/{n*200} Mean=123456"); sys.stdout.flush(); time.sleep(0.02)
print()
for idx, name in enumerate(profilesets, 1):
    sys.stdout.write(f"\rProfilesets ({idx}/{len(profilesets)}): {name} [=========>..........] 500/1000"); sys.stdout.flush(); time.sleep(0.01)
print()
if "save" in opts:
    p = players[0]
    with open(opts["save"], "w") as f:
        f.write(f'{p["cls"]}="{p["name"]}"\nspec={p["spec"]}\nlevel=90\n\n# This default action priority list is automatically created based on your character.\n'
                "# Executed before combat begins.\nactions.precombat=snapshot_stats\nactions.precombat+=/variable,name=st_planning,value=active_enemies=1&(raid_event.adds.in>15|!raid_event.adds.exists)\n"
                "# Executed every time the actor is available.\nactions=auto_attack\nactions+=/call_action_list,name=cooldowns\nactions+=/run_action_list,name=aoe,if=active_enemies>=3\n"
                "actions+=/heavy_strike,if=buff.empower.up&(cooldown.big_cd.remains>10|fight_remains<15),target_if=min:debuff.mark.remains\nactions+=/basic_strike\n"
                "actions.cooldowns=big_cd,if=fight_remains>30|target.time_to_die<20\nactions.aoe=whirl\n\nhead=,id=1\n")
    sys.exit(0)
rng = random.Random(len(players))
def player(p, base):
    fl = float(opts.get("max_time", 300))
    abil = [("heavy_strike","Heavy Strike","physical",0.35),("basic_strike","Basic Strike","physical",0.25),("shadow_burst","Shadow Burst","shadow",0.2),("melee","Melee","physical",0.12),("fire_dot","Fire Dot","fire",0.08)]
    stats = []
    for nm, sn, sc, sh in abil:
        st = {"name": nm, "spell_name": sn, "id": 1000+len(stats), "school": sc, "type": "damage", "num_executes": {"mean": 40*sh*10}, "compound_amount": base*sh*fl, "portion_aps": {"mean": base*sh}}
        if nm == "shadow_burst":
            st["compound_amount"] = base*0.12*fl
            st["children"] = [{"name": "shadow_burst_echo", "spell_name": "Shadow Burst Echo", "id": 2001, "school": "shadow", "type": "damage", "num_executes": {"mean": 20}, "compound_amount": base*0.08*fl}]
        stats.append(st)
    d = {"name": p["name"], "race": "human", "level": 90, "role": "attack", "specialization": (p["spec"] or "frost").title()+" "+{"deathknight":"Death Knight"}.get(p["cls"], p["cls"].title()),
         "talents": "C0PAAAAAAAAAAAAAAAAAAAAAAA", "collected_data": {"fight_length": {"mean": fl}, "dps": {"mean": base, "min": base*0.9, "max": base*1.1, "std_dev": base*0.03, "mean_std_dev": base*0.0004}, "dtps": {"mean": 0}, "hps": {"mean": 0}},
         "stats": stats, "buffs": [{"name": "bloodlust", "uptime": 13.3}, {"name": "empower", "uptime": 61.2}]}
    if opts.get("calculate_scale_factors") == "1":
        d["scale_factors"] = {"Str": 12.1, "Crit": 9.3, "Haste": 10.4, "Mastery": 8.8, "Vers": 9.9}
    return d
out = {"version": "1200-01", "sim": {"options": {"fight_style": opts.get("fight_style","Patchwerk"), "max_time": float(opts.get("max_time",300)), "iterations": 10000, "desired_targets": int(opts.get("desired_targets",1))},
       "players": [player(p, 200000 + i*3500) for i, p in enumerate(players)], "statistics": {"elapsed_cpu_seconds": 3.2},
       "profilesets": {"results": [{"name": k, "mean": 200000 + rng.uniform(-6000, 6000), "min": 1, "max": 2, "stddev": 6000, "mean_error": 150, "iterations": 5000} for k in profilesets]}},
       "notifications": []}
json.dump(out, open(opts.get("json2", "out.json"), "w"))
open(opts.get("html", "report.html"), "w").write("<html><body style='font-family:sans-serif'><h1>SimulationCraft report (fake)</h1><p>Test stand-in.</p></body></html>")
print("Report written.")
