// UI smoke test: drives every page against a running `OfflineSim -headless -port 8765`.
// Usage: node testdata/e2e.js [screenshot-dir]
const { chromium } = require('playwright');
const fs = require('fs');
const path = require('path');

const URL = 'http://127.0.0.1:8765/';
const S = (process.argv[2] || 'shots') + '/';

(async () => {
  fs.mkdirSync(S, { recursive: true });
  const b = await chromium.launch();
  const p = await b.newPage({ viewport: { width: 1440, height: 920 } });
  const errs = [];
  p.on('pageerror', (e) => errs.push(e.message));
  p.on('console', (m) => { if (m.type() === 'error') errs.push(m.text()); });
  const waitResult = async () => { await p.waitForURL(/#result\//, { timeout: 60000 }); await p.waitForTimeout(500); };

  await p.goto(URL + '#quick');
  await p.waitForTimeout(500);
  await p.fill('#profileInput', fs.readFileSync(path.join(__dirname, 'addon.txt'), 'utf8'));
  await p.waitForTimeout(600);
  await p.screenshot({ path: S + '1-quick.png', fullPage: true });
  await p.click('#runBtn');
  await p.waitForTimeout(400);
  await p.screenshot({ path: S + '2-running.png' });
  await waitResult();
  await p.screenshot({ path: S + '3-result.png', fullPage: true });

  await p.goto(URL + '#topgear'); await p.waitForTimeout(800);
  await p.click('#selectAllItems'); await p.waitForTimeout(800);
  console.log('combos:', await p.textContent('#comboCount'));
  await p.screenshot({ path: S + '4-topgear.png', fullPage: true });
  await p.click('#runBtn');
  await waitResult();
  await p.screenshot({ path: S + '5-topgear-result.png', fullPage: true });

  await p.goto(URL + '#stats'); await p.waitForTimeout(700);
  await p.click('#runBtn');
  await waitResult();
  await p.click('button[data-tab=weights]'); await p.waitForTimeout(300);
  await p.screenshot({ path: S + '6-weights.png', fullPage: true });

  await p.goto(URL + '#library/deathknight/frost'); await p.waitForTimeout(1500);
  await p.screenshot({ path: S + '7-library.png', fullPage: true });
  await p.click('#aplDup'); await p.waitForTimeout(800);
  await p.fill('#aplText', (await p.inputValue('#aplText')) + 'actions+=/extra_thing,if=x>1\n');
  await p.fill('#aplName', 'Frost test APL');
  await p.click('#aplSave'); await p.waitForTimeout(800);
  await p.screenshot({ path: S + '8-library-custom.png', fullPage: true });

  await p.goto(URL + '#apl'); await p.waitForTimeout(800);
  for (const x of await p.$$('#compareList input')) await x.check();
  await p.click('#runBtn');
  await waitResult();
  await p.click('button[data-tab=compare]').catch(() => {});
  await p.screenshot({ path: S + '9-compare.png', fullPage: true });

  await p.goto(URL + '#results'); await p.waitForTimeout(700);
  await p.screenshot({ path: S + '10-history.png', fullPage: true });
  await p.goto(URL + '#settings'); await p.waitForTimeout(700);
  await p.screenshot({ path: S + '11-settings.png', fullPage: true });
  console.log('errors:', JSON.stringify(errs));
  await b.close();
  if (errs.length) process.exit(1);
})().catch((e) => { console.error(e); process.exit(1); });
