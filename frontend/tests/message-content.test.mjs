import assert from 'node:assert/strict';
import test from 'node:test';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { load } from './load-source.mjs';

const { MessageText, MessageSources } = load('pages/MessageContent.tsx');
const renderText = (text, assistant = false) => renderToStaticMarkup(React.createElement(MessageText, { text, assistant }));
const renderSources = sources => renderToStaticMarkup(React.createElement(MessageSources, { sources }));

for (const assistant of [false, true]) {
  const sender = assistant ? 'assistant' : 'user';
  test(`${sender} messages render headings, emphasis, lists and code without interpreting code as HTML`, () => {
    const html = renderText('## План\n\n**Важно** и *подсказка*\n\n- Первый\n- Второй\n\n`<button>`\n\n```html\n<script>alert(1)</script>\n```', assistant);
    assert.match(html, /<h2>План<\/h2>/);
    assert.match(html, /<strong>Важно<\/strong>/);
    assert.match(html, /<em>подсказка<\/em>/);
    assert.match(html, /<ul>\s*<li>Первый<\/li>\s*<li>Второй<\/li>\s*<\/ul>/);
    assert.match(html, /<code>&lt;button&gt;<\/code>/);
    assert.match(html, /<pre><code[^>]*>&lt;script&gt;alert\(1\)&lt;\/script&gt;/);
    assert.doesNotMatch(html, /<script|<button/i);
  });

  test(`${sender} messages wrap Markdown tables and retain explicit line breaks`, () => {
    const html = renderText('| Ресурс | Доступ |\n| --- | --- |\n| Git | Read |\n\nПервая строка\nВторая строка', assistant);
    assert.match(html, /<div class="message-table-scroll"><table>/);
    assert.match(html, /<th>Ресурс<\/th>/);
    assert.match(html, /<td>Read<\/td>/);
    assert.match(html, /<\/table><\/div>/);
    assert.match(html, /Первая строка<br>\nВторая строка/);
  });

  test(`${sender} HTTP links open separately without access to the original window`, () => {
    const html = renderText('[Документ](https://docs.example/guide?a=1&b=2) и http://example.com/help', assistant);
    const links = html.match(/<a\b[^>]*>/g) || [];
    assert.equal(links.length, 2);
    for (const link of links) {
      assert.match(link, /target="_blank"/);
      assert.match(link, /rel="noopener noreferrer"/);
    }
    assert.match(html, /href="https:\/\/docs.example\/guide\?a=1&amp;b=2"/);
  });

  test(`${sender} messages keep raw HTML inert and never load Markdown images`, () => {
    const html = renderText('<img src="https://example.com/track" onerror="alert(1)">\n<iframe src="https://example.com"></iframe>\n![image](https://example.com/pixel.png)', assistant);
    assert.doesNotMatch(html, /<(?:img|iframe|script)\b/i);
    assert.doesNotMatch(html, /<[^>]+\bonerror\s*=/i);
    assert.match(html, /&lt;img/);
  });

  for (const url of ['javascript:alert%281%29', 'JaVaScRiPt:alert%281%29', 'java&#x73;cript:alert%281%29', 'data:text/html,hello', 'file:///etc/passwd', 'vbscript:hello', '//evil.example/path']) {
    test(`${sender} rejects a non-HTTP Markdown target: ${url}`, () => {
      assert.doesNotMatch(renderText(`[ссылка](${url})`, assistant), /<a\b/i);
    });
  }
}

test('service citations are removed only from assistant messages, preserving the user original', () => {
  const text = 'Текст [source:demo] и [важно]';
  assert.match(renderText(text), /\[source:demo\]/);
  assert.doesNotMatch(renderText(text, true), /source:demo/);
  assert.match(renderText(text, true), /\[важно\]/);
});

test('source deduplication preserves distinct materials and removes checklist entries before deduplicating', () => {
  const html = renderSources([
    { id: 'shared', title: '[ЧЕК-ЛИСТ] Скрытый', url: 'https://example.com/hidden' },
    { id: 'shared', title: 'Инструкция', url: 'https://example.com/guide' },
    { id: 'shared', title: 'Инструкция', url: 'https://example.com/guide' },
    { id: 'other', title: 'Другая инструкция', url: 'https://example.com/other' },
  ]);
  assert.equal((html.match(/<li>/g) || []).length, 2);
  assert.match(html, /href="https:\/\/example.com\/guide"/);
  assert.match(html, /href="https:\/\/example.com\/other"/);
  assert.doesNotMatch(html, /Скрытый|example.com\/hidden/);
});

test('unsafe source URLs keep their escaped titles but never become clickable', () => {
  for (const url of ['javascript:alert(1)', 'data:text/html,hello', '//evil.example', 'file:///tmp/doc', 'not a URL']) {
    const html = renderSources([{ id: 'unsafe', title: '<img src=x onerror=alert(1)>', url }]);
    assert.doesNotMatch(html, /<a\b|<img\b/i);
    assert.match(html, /&lt;img/);
  }
  assert.equal(renderSources([]), '');
});
