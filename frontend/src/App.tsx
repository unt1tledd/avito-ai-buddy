import React, { useState, useEffect, useCallback } from 'react';
import { ChatPage } from './pages/ChatPage';
import {
  INITIAL_ONBOARDING_STEPS,
  OnboardingStep,
  AccessTask,
  fetchOnboardingPlan,
  fetchAccessTasks,
} from './lib/api';

const recomputeCurrentFlags = (list: OnboardingStep[]): OnboardingStep[] => {
  const firstUncompletedIdx = list.findIndex((s) => !s.completed);
  return list.map((s, idx) => ({
    ...s,
    current: firstUncompletedIdx !== -1 && idx === firstUncompletedIdx,
  }));
};

const normalizeInitialSteps = (rawSteps: OnboardingStep[]): OnboardingStep[] =>
  recomputeCurrentFlags(
    rawSteps.map((step) => ({
      ...step,
      completed: false,
    }))
  );

const LOGO_DOT_COLORS = ['#04E061', '#00AAFF', '#965EEB', '#FF4053'] as const;

export const App: React.FC = () => {
  const [steps, setSteps] = useState<OnboardingStep[]>(() =>
    normalizeInitialSteps(INITIAL_ONBOARDING_STEPS)
  );
  const [tasks, setTasks] = useState<AccessTask[]>([]);
  const [theme, setTheme] = useState<'light' | 'dark'>('light');

  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme);
  }, [theme]);

  useEffect(() => {
    fetchOnboardingPlan().then((loaded) => setSteps(normalizeInitialSteps(loaded)));
    fetchAccessTasks().then(setTasks);
  }, []);

  const handleToggleStep = useCallback((stepId: string) => {
    setSteps((prev) => {
      const target = prev.find((s) => s.step_id === stepId);
      const nextCompleted = target ? !target.completed : true;

      if (target?.resource_id && !nextCompleted) {
        setTasks((prevTasks) =>
          prevTasks.filter((t) => t.resource_id !== target.resource_id)
        );
      }

      return recomputeCurrentFlags(
        prev.map((s) =>
          s.step_id === stepId ? { ...s, completed: nextCompleted } : s
        )
      );
    });
  }, []);

  const handleProposalConfirmed = useCallback((resourceId: string, issueKey: string) => {
    const newTask: AccessTask = {
      task_id: `task-${issueKey}`,
      proposal_id: `prop-${issueKey}`,
      resource_id: resourceId,
      title: `Выдать стажёру доступ к пространству команды (${resourceId})`,
      assignee: 'Тимлид команды',
      tracker_issue_key: issueKey,
      status: 'approved',
      created_at: 'Сегодня',
    };

    setTasks((prev) =>
      prev.some((t) => t.resource_id === resourceId) ? prev : [newTask, ...prev]
    );

    setSteps((prev) =>
      recomputeCurrentFlags(
        prev.map((s) =>
          s.resource_id === resourceId ? { ...s, completed: true } : s
        )
      )
    );
  }, []);

  const isDark = theme === 'dark';

  return (
    <div className="app-root">
      <header className="app-header">
        <div className="aibuddy-logo-pill">
          <span className="aibuddy-logo-dots" aria-hidden="true">
            {LOGO_DOT_COLORS.map((color) => (
              <span
                key={color}
                className="aibuddy-logo-dot"
                style={{ background: color }}
              />
            ))}
          </span>
          <span>AI BUDDY</span>
        </div>

        <button
          type="button"
          className={`theme-toggle-btn${isDark ? ' theme-toggle-btn--dark' : ''}`}
          onClick={() => setTheme((prev) => (prev === 'light' ? 'dark' : 'light'))}
          title={isDark ? 'Переключить на светлую тему' : 'Переключить на тёмную тему'}
          aria-label="Переключить тему оформления"
        >
          <span className="theme-toggle-thumb">
            {isDark ? (
              <svg viewBox="0 0 24 24" aria-hidden="true">
                <circle cx="12" cy="12" r="4.2" />
                <path d="M12 2.5v2.3M12 19.2v2.3M4.9 4.9l1.6 1.6M17.5 17.5l1.6 1.6M2.5 12h2.3M19.2 12h2.3M4.9 19.1l1.6-1.6M17.5 6.5l1.6-1.6" />
              </svg>
            ) : (
              <svg viewBox="0 0 24 24" aria-hidden="true">
                <path d="M20.2 14.8A8.5 8.5 0 0 1 9.2 3.8a8.5 8.5 0 1 0 11 11Z" />
              </svg>
            )}
          </span>
        </button>
      </header>

      <ChatPage
        steps={steps}
        tasks={tasks}
        onToggleStep={handleToggleStep}
        onProposalConfirmed={handleProposalConfirmed}
      />
    </div>
  );
};

export default App;
