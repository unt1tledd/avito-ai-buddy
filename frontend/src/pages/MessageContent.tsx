import React, { useMemo } from 'react';
import MarkdownIt from 'markdown-it';
import type { SourceLink } from '../lib/api';

export function assistantDisplayText(text:string):string {
  const cleaned=text.replace(/[ \t]*\[source:[^\]\r\n]*\]/gi,'').trim();
  const decision=cleaned.match(/^Статус задачи (\S+) измен[её]н:\s*(approved|rejected|pending_dispatch|pending_approval|pending|failed)\.?$/i);
  if(!decision)return cleaned;
  const statuses:Record<string,string>={approved:'одобрено',rejected:'отклонено',pending_dispatch:'в очереди на отправку',pending_approval:'ожидает согласования',pending:'ожидает согласования',failed:'не удалось отправить'};
  return `Заявка №${decision[1]}: ${statuses[decision[2].toLowerCase()]}.`;
}

function httpUrl(value:string):URL|null {
  try {const url=new URL(value);return url.protocol==='https:' || url.protocol==='http:'?url:null;} catch {return null;}
}

export function MessageLink({value}:{value:string}) {
  const url=httpUrl(value);
  return url?<a className="message-inline-link" href={url.href} target="_blank" rel="noopener noreferrer">{value}</a>:<>{value}</>;
}

// Raw HTML stays text. Only HTTP(S) links are rendered as clickable URLs.
const markdown=new MarkdownIt({html:false,linkify:true,breaks:true});
markdown.validateLink=value=>httpUrl(value)!==null;
markdown.disable('image');
markdown.renderer.rules.link_open=(tokens,index,options,_env,renderer)=>{
  tokens[index].attrSet('target','_blank');
  tokens[index].attrSet('rel','noopener noreferrer');
  tokens[index].attrJoin('class','message-inline-link');
  return renderer.renderToken(tokens,index,options);
};
markdown.renderer.rules.table_open=()=>'<div class="message-table-scroll"><table>';
markdown.renderer.rules.table_close=()=>'</table></div>';

export function MessageText({text,assistant=false}:{text:string;assistant?:boolean}) {
  const display=assistant?assistantDisplayText(text):text;
  const html=useMemo(()=>markdown.render(display),[display]);
  return <div className="message-text message-markdown" dangerouslySetInnerHTML={{__html:html}}/>;
}

export function sourceTitle(title:string):string {
  return title.replace(/\[ЧЕК[\s-]*ЛИСТ\]\s*/giu,'').trim() || 'Материал базы знаний';
}

export function MessageSources({sources}:{sources:SourceLink[]}) {
  const visible=sources.filter(source=>!(/\[ЧЕК[\s\u2010-\u2015-]*ЛИСТ\]/iu.test(source.title || '')));
  const unique=[...new Map(visible.map(source=>[source.id || source.url || source.title,source])).values()];
  if(!unique.length)return null;
  return <section className="message-sources" aria-label="Источники ответа"><span className="message-sources-heading">{unique.length===1?'Источник':'Источники'}</span><ul>{unique.map((source,index)=>{
    const url=httpUrl(source.url || '');
    const content=<><span className="message-source-icon" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round"><path d="M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9Zm0 0v6h6M8 13h8M8 17h5"/></svg></span><span className="message-source-label"><span>{sourceTitle(source.title || '')}</span>{url && <small>{url.hostname}</small>}</span>{url && <span className="message-source-arrow" aria-hidden="true">↗</span>}</>;
    return <li key={source.id || index}>{url?<a className="message-source-card" href={url.href} target="_blank" rel="noopener noreferrer">{content}</a>:<div className="message-source-card">{content}</div>}</li>;
  })}</ul></section>;
}
