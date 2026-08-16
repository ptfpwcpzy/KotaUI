import fs from 'node:fs';
import vm from 'node:vm';

const html = fs.readFileSync(new URL('../public/index.html', import.meta.url), 'utf8');
const match = html.match(/<script>([\s\S]*?)<\/script>/i);
if (!match) throw new Error('未找到内联 JavaScript');
new vm.Script(match[1], { filename: 'public/index.html:inline-script' });
console.log('Inline JavaScript syntax: PASS');
