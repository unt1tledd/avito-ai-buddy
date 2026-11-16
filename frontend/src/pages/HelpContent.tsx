import React from 'react';

export function HelpContent({onClose}:{onClose:()=>void}) {
  return <div className="buddy-help-content">
    <button type="button" className="buddy-icon-button buddy-help-close" autoFocus aria-label="Закрыть помощь" onClick={onClose}>
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" aria-hidden="true"><path d="m6 6 12 12M18 6 6 18"/></svg>
    </button>
    <header>
      <h2 id="buddy-help-title">Помощь с AI Buddy</h2>
      <p className="buddy-help-intro">Здесь собрали подсказки, которые помогут освоиться в приложении и продолжить адаптацию.</p>
    </header>
    <div className="buddy-help-grid">
      <section className="buddy-help-section" aria-labelledby="help-usage-title">
        <h3 id="help-usage-title">Как пользоваться</h3>
        <ul>
          <li><strong>Чат.</strong> Задавай рабочие вопросы и открывай источники под ответом. Enter отправляет сообщение, Shift + Enter добавляет новую строку. В сообщениях можно использовать Markdown.</li>
          <li><strong>Roadmap.</strong> Выбирай трек, открывай шаги и отмечай выполненные галочкой. Если нужна подсказка, спроси AI Buddy о выбранном шаге.</li>
          <li><strong>Заявки.</strong> Следи за отправленными запросами на доступ и решениями по ним в разделе «Заявки».</li>
        </ul>
      </section>
      <section className="buddy-help-section" aria-labelledby="help-contact-title">
        <h3 id="help-contact-title">К кому обратиться</h3>
        <p>Нужна помощь с приложением, нашёл ошибку или хочешь предложить улучшение? Напиши нам на почту:</p>
        <a className="buddy-help-email" href="mailto:faruk.cafer@bk.ru">faruk.cafer@bk.ru</a>
        <p>Вопросы по задачам команды и порядку адаптации также можно обсудить со своим бадди или наставником.</p>
      </section>
      <section className="buddy-help-section" aria-labelledby="help-problem-title">
        <h3 id="help-problem-title">Если что-то не работает</h3>
        <p>Попробуй повторить действие или обновить страницу. Если проблема осталась, опиши в письме, на какой странице она возникла, что ты делал и какого результата ожидал.</p>
        <p>Приложи скриншот и укажи примерное время ошибки. Перед отправкой скрой личные данные и другую закрытую информацию.</p>
      </section>
      <section className="buddy-help-section" aria-labelledby="help-about-title">
        <h3 id="help-about-title">Об AI Buddy</h3>
        <p>AI Buddy помогает найти информацию по стажировке, разобраться в рабочих процессах и пройти адаптацию.</p>
        <p>Ответы могут быть неточными: сверяй важные детали с источниками и уточняй их у бадди. Не отправляй в чат и письма пароли, токены и ключи доступа.</p>
      </section>
    </div>
    <footer><button type="button" className="buddy-action-button" onClick={onClose}>Понятно</button></footer>
  </div>;
}
