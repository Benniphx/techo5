// A page reader for the TECHO5 voice assistant: loads a page in headless Chromium, scripts and all,
// and returns the HTML it ends up with, for pages that are empty until their scripts run.
// GET /read?url=https://...  ->  200 text/html (X-Final-URL: where it ended up), or 4xx/5xx text.
// The container's firewall (entrypoint.sh) keeps the browser off private and 100.64.0.0/10 addresses.
const http = require('http');
const { chromium } = require('playwright');

const PORT = 8891;
const MAX_HTML = 3 << 20;   // what echod reads of a page at most
const NAV_TIMEOUT = 15000;  // loading the page
const SETTLE = 4000;        // waiting for its scripts to fill it in
const MAX_BUSY = 2;         // pages at once; more get 503
const UA = 'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0 Safari/537.36';

let browser = null;
let busy = 0;

async function getBrowser() {
	if (!browser || !browser.isConnected()) {
		browser = await chromium.launch();
	}
	return browser;
}

async function read(target) {
	const ctx = await (await getBrowser()).newContext({ userAgent: UA, serviceWorkers: 'block', locale: 'en-US' });
	try {
		const page = await ctx.newPage();
		await page.route('**/*', (route) => {
			const req = route.request();
			const proto = new URL(req.url()).protocol;
			if ((proto !== 'http:' && proto !== 'https:') || ['image', 'media', 'font'].includes(req.resourceType())) {
				return route.abort();
			}
			return route.continue();
		});
		const resp = await page.goto(target, { waitUntil: 'domcontentloaded', timeout: NAV_TIMEOUT });
		await page.waitForLoadState('networkidle', { timeout: SETTLE }).catch(() => {});
		let html = await page.content();
		if (html.length > MAX_HTML) html = html.slice(0, MAX_HTML);
		return { status: resp ? resp.status() : 200, html, final: page.url() };
	} finally {
		await ctx.close().catch(() => {});
	}
}

http.createServer(async (req, res) => {
	const u = new URL(req.url, 'http://x');
	if (u.pathname === '/health') {
		res.writeHead(200, { 'Content-Type': 'text/plain' });
		return res.end('ok');
	}
	if (u.pathname !== '/read' || req.method !== 'GET') {
		res.writeHead(404);
		return res.end('not found');
	}
	let target;
	try {
		target = new URL(u.searchParams.get('url') || '');
	} catch {
		res.writeHead(400);
		return res.end('not a web address');
	}
	if (target.protocol !== 'http:' && target.protocol !== 'https:') {
		res.writeHead(400);
		return res.end('not a web address');
	}
	if (busy >= MAX_BUSY) {
		res.writeHead(503);
		return res.end('busy');
	}
	busy++;
	const started = Date.now();
	try {
		const r = await read(target.href);
		res.writeHead(r.status >= 400 ? r.status : 200, { 'Content-Type': 'text/html; charset=utf-8', 'X-Final-URL': r.final });
		res.end(r.html);
		console.log(`${new Date().toISOString()} ${r.status} ${Date.now() - started}ms ${target.host}`);
	} catch (e) {
		res.writeHead(502, { 'Content-Type': 'text/plain' });
		res.end(String(e.message || e).split('\n')[0].slice(0, 200));
		console.log(`${new Date().toISOString()} error ${Date.now() - started}ms ${target.host}: ${String(e.message || e).split('\n')[0]}`);
	} finally {
		busy--;
	}
}).listen(PORT, () => console.log(`reader on :${PORT}`));
