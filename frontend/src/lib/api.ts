import { getAccessToken } from './cookies';

export interface SourceLink {
  id: string;
  title: string;
  url: string;
}

export interface Proposal {
  proposal_id: string;
  title: string;
  role_label: string;
  resource_id: string;
  resource_label: string;
  access_level: string;
  status: 'draft' | 'pending_approval' | 'approved';
  tracker_issue_key?: string;
}

export interface ChatMessage {
  id: string;
  sender: 'user' | 'assistant';
  text: string;
  timestamp: string;
  idempotency_key?: string;
  sources?: SourceLink[];
  proposal?: Proposal;
  error?: boolean;
}

export interface OnboardingStep {
  step_id: string;
  step_number: number;
  title: string;
  description: string;
  period: '1 неделя' | '2 неделя' | '1 месяц' | '3+ месяца';
  resource_id?: string;
  completed: boolean;
  current?: boolean;
}

export interface AccessTask {
  task_id: string;
  proposal_id: string;
  resource_id: string;
  title: string;
  assignee: string;
  tracker_issue_key: string;
  status: 'pending_approval' | 'approved';
  created_at: string;
}

export interface HistoryItem {
  id: string;
  dayGroup: 'СЕГОДНЯ' | 'ВЧЕРА (ДЕНЬ 1)';
  timeLabel: string;
  title: string;
  badgeText: string;
  badgeTone: 'green' | 'blue' | 'gray';
  messages: ChatMessage[];
}

// Начальные шаги из таблицы «Чек-лист стажёра Авито»
export const INITIAL_ONBOARDING_STEPS: OnboardingStep[] = [
  {
    step_id: 'step-1',
    step_number: 1,
    title: 'Познакомься с командой и чатами',
    description: 'Добавиться во все командные каналы в мессенджере и познакомиться с коллегами.',
    period: '1 неделя',
    completed: true,
  },
  {
    step_id: 'step-2',
    step_number: 2,
    title: 'Работа с наставником (назначить 1:1 и конспект)',
    description: 'Договориться о регулярных встречах с бадди и завести документ для заметок и OKR.',
    period: '1 неделя',
    completed: true,
  },
  {
    step_id: 'step-3',
    step_number: 3,
    title: 'Запроси доступ в пространство своего кластера (Confluence / Jira)',
    description: 'Получить доступ к командному пространству team-space-a и репозиторию микросервиса.',
    period: '1 неделя',
    resource_id: 'team-space-a',
    completed: false,
    current: true,
  },
  {
    step_id: 'step-4',
    step_number: 4,
    title: 'Запроси у куратора инструмент отслеживания прогресса',
    description: 'Внести в трекер стажировки цели, задачи и ожидания на первые месяцы.',
    period: '2 неделя',
    completed: false,
  },
  {
    step_id: 'step-5',
    step_number: 5,
    title: 'Изучи документы и мануалы своей вертикали',
    description: 'Разобрать архитектурные схемы и стандарты разработки из AvitoTech Playbook.',
    period: '2 неделя',
    completed: false,
  },
  {
    step_id: 'step-6',
    step_number: 6,
    title: 'Изучи основы защиты персональных данных и КИ',
    description: 'Ознакомиться с лендингом «Защита данных» и политикой конфиденциальной информации.',
    period: '1 месяц',
    completed: false,
  },
  {
    step_id: 'step-7',
    step_number: 7,
    title: 'Подготовься к экваторной встрече и выпускному',
    description: 'Оформить презентацию с промежуточными результатами, провести репетицию с бадди.',
    period: '3+ месяца',
    completed: false,
  },
];

export const INITIAL_HISTORY_ITEMS: HistoryItem[] = [
  {
    id: 'hist-access',
    dayGroup: 'СЕГОДНЯ',
    timeLabel: 'Сегодня, 14:20',
    title: '«Запрос доступа в пространство кластера»',
    badgeText: 'Заявка подтверждена',
    badgeTone: 'green',
    messages: [
      {
        id: 'm-h1',
        sender: 'user',
        text: 'Запрос доступа в пространство кластера',
        timestamp: '14:20',
      },
      {
        id: 'm-h2',
        sender: 'assistant',
        text: 'Заявка на доступ в пространство твоего кластера (Confluence и Jira) оформлена и подтверждена.',
        timestamp: '14:20',
        proposal: {
          proposal_id: 'prop-1042',
          title: 'Заявка на доступ: Пространство кластера Backend-команды',
          role_label: 'Стажёр',
          resource_id: 'team-space-a',
          resource_label: 'Confluence + Jira Space (team-space-a)',
          access_level: 'read_write',
          status: 'approved',
          tracker_issue_key: '1042',
        },
      },
    ],
  },
  {
    id: 'hist-dayoff',
    dayGroup: 'СЕГОДНЯ',
    timeLabel: 'Сегодня, 11:05',
    title: '«Как оформить отгул на 1 день?»',
    badgeText: 'Есть ответ',
    badgeTone: 'blue',
    messages: [
      {
        id: 'm-d1',
        sender: 'user',
        text: 'Как оформить отгул?',
        timestamp: '11:05',
      },
      {
        id: 'm-d2',
        sender: 'assistant',
        text: 'Чтобы оформить отгул на 1 день, выполни три простых шага:\n1. Предупреди команду: согласуй дату со своим наставником и руководителем в рабочем чате.\n2. Оформи заявку: зайди на внутренний портал в раздел «Отсутствия» и выбери нужную дату.\n3. Поставь статус: отметь день отсутствия в календаре и мессенджере.',
        timestamp: '11:05',
        sources: [
          {
            id: 's1',
            title: '[1] Регламент сервиса Отсутствия',
            url: 'https://github.com/avito-tech/playbook',
          },
          {
            id: 's2',
            title: '[2] Playbook: правила команды',
            url: 'https://github.com/avito-tech/playbook',
          },
        ],
      },
    ],
  },
  {
    id: 'hist-chats',
    dayGroup: 'ВЧЕРА (ДЕНЬ 1)',
    timeLabel: 'Вчера',
    title: '«В какие чаты и каналы нужно добавиться?»',
    badgeText: 'Просмотрено',
    badgeTone: 'gray',
    messages: [
      {
        id: 'm-c1',
        sender: 'user',
        text: 'В какие чаты и каналы нужно добавиться в первую неделю?',
        timestamp: 'Вчера',
      },
      {
        id: 'm-c2',
        sender: 'assistant',
        text: 'В первую неделю попроси наставника (бадди) добавить тебя в командный чат, канал кластера, канал анонсов AvitoTech и чат комьюнити стажёров Avito Start.',
        timestamp: 'Вчера',
        sources: [
          {
            id: 's3',
            title: '[1] Чек-лист стажёра Авито (1 неделя)',
            url: 'https://docs.google.com/spreadsheets/d/1qKFqOzfSZ2jEz57jBSmq7rukDXi_tRfWohgemViceV0/edit?usp=sharing',
          },
        ],
      },
    ],
  },
  {
    id: 'hist-1on1',
    dayGroup: 'ВЧЕРА (ДЕНЬ 1)',
    timeLabel: 'Вчера',
    title: '«Как подготовиться к первой встрече 1:1 с бадди?»',
    badgeText: 'Просмотрено',
    badgeTone: 'gray',
    messages: [
      {
        id: 'm-o1',
        sender: 'user',
        text: 'Как подготовиться к первой встрече 1:1 с бадди?',
        timestamp: 'Вчера',
      },
      {
        id: 'm-o2',
        sender: 'assistant',
        text: 'Заведи единый конспект стажировки, выпиши вопросы по прошедшим командным встречам и обсуди с бадди ожидания и OKR на испытательный срок.',
        timestamp: 'Вчера',
      },
    ],
  },
];

// Ручка 1: GET /api/v1/chat/history
export async function fetchChatHistory(): Promise<ChatMessage[]> {
  try {
    const res = await fetch('/api/v1/chat/history', {
      headers: { Authorization: `Bearer ${getAccessToken()}` },
    });
    if (!res.ok) throw new Error('Backend offline');
    const data = await res.json();
    return data.messages || [];
  } catch {
    return [];
  }
}

// Ручка 2: POST /api/v1/chat/messages
export async function sendChatMessage(
  text: string,
  idempotencyKey: string,
  simulateError = false
): Promise<ChatMessage> {
  if (simulateError) {
    throw new Error('AI_SERVICE_TIMEOUT');
  }

  try {
    const res = await fetch('/api/v1/chat/messages', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${getAccessToken()}`,
        'Idempotency-Key': idempotencyKey,
      },
      body: JSON.stringify({ text, idempotency_key: idempotencyKey }),
    });
    if (res.ok) {
      const data = await res.json();
      return {
        id: 'msg-' + Date.now(),
        sender: 'assistant',
        text: data.message,
        timestamp: new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
        sources: data.sources,
        proposal: data.proposal,
      };
    }
  } catch {
    // Используем локальную демонстрационную логику, если Go-бэкенд не запущен
  }

  await new Promise((r) => setTimeout(r, 450));
  const lower = text.toLowerCase();

  if (lower.includes('отгул') || lower.includes('отпуск')) {
    return {
      id: 'msg-' + Date.now(),
      sender: 'assistant',
      text: 'Чтобы оформить отгул на 1 день, выполни три простых шага:\n1. Предупреди команду: согласуй дату со своим наставником и руководителем в рабочем чате.\n2. Оформи заявку: зайди на внутренний портал в раздел «Отсутствия» и выбери нужную дату.\n3. Поставь статус: отметь день отсутствия в календаре и мессенджере.',
      timestamp: new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
      sources: [
        {
          id: 's-1',
          title: '[1] Регламент сервиса Отсутствия',
          url: 'https://github.com/avito-tech/playbook',
        },
        {
          id: 's-2',
          title: '[2] Playbook: правила команды',
          url: 'https://github.com/avito-tech/playbook',
        },
      ],
    };
  }

  if (lower.includes('текущий шаг') || lower.includes('доступ') || lower.includes('кластер')) {
    return {
      id: 'msg-' + Date.now(),
      sender: 'assistant',
      text: 'Твой текущий шаг 1-й недели — получить доступ в пространство кластера в Confluence и Jira. Я уже подготовил заявку для твоей команды:',
      timestamp: new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
      proposal: {
        proposal_id: 'prop-1042',
        title: 'Заявка на доступ: Пространство кластера Backend-команды',
        role_label: 'Стажёр',
        resource_id: 'team-space-a',
        resource_label: 'Confluence + Jira Space',
        access_level: 'read_write',
        status: 'draft',
        tracker_issue_key: '1042',
      },
    };
  }

  if (lower.includes('персональн') || lower.includes('защит') || lower.includes('безопасност')) {
    return {
      id: 'msg-' + Date.now(),
      sender: 'assistant',
      text: 'При работе с персональными данными и внутренней информацией используй только корпоративные сервисы Авито и не загружай рабочие файлы на сторонние сайты.',
      timestamp: new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
      sources: [
        {
          id: 's-sec-1',
          title: '[1] Лендинг «Защита данных»',
          url: 'https://avito.tech/',
        },
        {
          id: 's-sec-2',
          title: '[2] Памятка по безопасности',
          url: 'https://github.com/avito-tech/playbook',
        },
      ],
    };
  }

  return {
    id: 'msg-' + Date.now(),
    sender: 'assistant',
    text: `Я проверил базу знаний AvitoTech Playbook и твой план адаптации по запросу «${text}». Текущий фокус первой недели — завершить настройку доступов в Confluence и Jira (ресурс team-space-a).`,
    timestamp: new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
    sources: [
      {
        id: 's-gen-1',
        title: '[1] AvitoTech Playbook',
        url: 'https://github.com/avito-tech/playbook',
      },
    ],
  };
}

// Ручка 3: POST /api/v1/chat/proposals/{proposal_id}/confirm
export async function confirmAccessProposal(proposalId: string): Promise<{
  task_id: string;
  tracker_issue_key: string;
  status: 'pending_approval' | 'approved';
}> {
  try {
    const res = await fetch(`/api/v1/chat/proposals/${proposalId}/confirm`, {
      method: 'POST',
      headers: { Authorization: `Bearer ${getAccessToken()}` },
    });
    if (res.ok) {
      return await res.json();
    }
  } catch {
    // Fallback для локального демо
  }
  await new Promise((r) => setTimeout(r, 400));
  return {
    task_id: 'task-1042',
    tracker_issue_key: '1042',
    status: 'approved',
  };
}

// Ручка 4: GET /api/v1/onboarding/plan
export async function fetchOnboardingPlan(): Promise<OnboardingStep[]> {
  try {
    const res = await fetch('/api/v1/onboarding/plan', {
      headers: { Authorization: `Bearer ${getAccessToken()}` },
    });
    if (res.ok) {
      const data = await res.json();
      if (Array.isArray(data.steps) && data.steps.length > 0) {
        return data.steps;
      }
    }
  } catch {
    // Fallback
  }
  return INITIAL_ONBOARDING_STEPS;
}

// Ручка 5: GET /api/v1/tasks?kind=access_request
export async function fetchAccessTasks(): Promise<AccessTask[]> {
  try {
    const res = await fetch('/api/v1/tasks?kind=access_request', {
      headers: { Authorization: `Bearer ${getAccessToken()}` },
    });
    if (res.ok) {
      const data = await res.json();
      if (Array.isArray(data)) return data;
    }
  } catch {
    // Fallback
  }
  return [];
}
