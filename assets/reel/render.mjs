// Renders index.html to a frame-exact MP4: seek(t) per frame -> PNG -> ffmpeg.
// Usage: node render.mjs [out.mp4] [--fps 30] [--at 12.5,30  (stills only, for checking)]
//        node render.mjs --serve   (live presenter mode: open the printed URL and screen-record)
import { createServer } from "node:http";
import { readFile, writeFile } from "node:fs/promises";
import { spawn } from "node:child_process";
import { extname, join, normalize } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright-core";

const args = process.argv.slice(2);
const opt = (name, def) => { const i = args.indexOf(name); return i < 0 ? def : args.splice(i, 2)[1]; };
const FPS = Number(opt("--fps", 30));
const AT = opt("--at", null);
const SERVE = args.includes("--serve");
const OUT = args.find(a => !a.startsWith("--")) ?? "reel.mp4";

// static server over assets/ so the page and the diagram iframes share one origin (seek reaches into them)
const ROOT = fileURLToPath(new URL("..", import.meta.url));
const TYPES = { ".html": "text/html", ".svg": "image/svg+xml" };
const server = createServer(async (req, res) => {
  const path = normalize(decodeURIComponent(new URL(req.url, "http://x").pathname));
  const body = await readFile(join(ROOT, path)).catch(() => null);
  if (!body) return res.writeHead(404).end();
  res.writeHead(200, { "content-type": TYPES[extname(path)] ?? "application/octet-stream" }).end(body);
}).listen(SERVE ? 8765 : 0, "127.0.0.1");
await new Promise(ok => server.once("listening", ok));
const URL_ = `http://127.0.0.1:${server.address().port}/reel/index.html`;
if (SERVE) {
  console.log(`live reel: ${URL_}  (←/→ scenes, space pause, ctrl+c to stop)`);
  await new Promise(() => {});
}

const browser = await chromium.launch({ channel: "chrome" });
const page = await browser.newPage({ viewport: { width: 1080, height: 1920 }, colorScheme: "dark" });
await page.goto(`${URL_}?render`);
await page.evaluate(() => window.ready);
const stage = page.locator("#stage");
const shot = async t => { await page.evaluate(t => seek(t), t); return stage.screenshot({ type: "png", animations: "allow" }); };

if (AT) {
  for (const t of AT.split(",").map(Number)) await writeFile(`still-${t}.png`, await shot(t));
} else {
  const total = await page.evaluate(() => TOTAL);
  const ff = spawn("ffmpeg", ["-y", "-loglevel", "error", "-f", "image2pipe", "-framerate", String(FPS), "-i", "-",
    "-c:v", "libx264", "-pix_fmt", "yuv420p", "-crf", "18", "-preset", "slow", "-movflags", "+faststart", OUT], { stdio: ["pipe", "inherit", "inherit"] });
  const n = Math.round(total * FPS);
  for (let i = 0; i < n; i++) {
    if (!ff.stdin.write(await shot(i / FPS))) await new Promise(ok => ff.stdin.once("drain", ok));
    if (i % FPS === 0) process.stdout.write(`\r${(i / FPS).toFixed(0)}/${total}s`);
  }
  ff.stdin.end();
  await new Promise((ok, fail) => ff.on("close", c => (c ? fail(new Error(`ffmpeg exit ${c}`)) : ok())));
  await writeFile(OUT.replace(/\.mp4$/, ".srt"), await page.evaluate(() => SCENES.filter(s => s.cap).map((s, i) => {
    const ts = t => new Date(t * 1000).toISOString().slice(11, 23).replace(".", ",");
    return `${i + 1}\n${ts(s.start + 1.2)} --> ${ts(s.end - .3)}\n${s.cap.replace(/<[^>]+>/g, "")}\n`;
  }).join("\n")));
  console.log(`\nwrote ${OUT}`);
}
await browser.close();
server.close();
