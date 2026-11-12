import React, { useState, useEffect } from 'react';
import { ChatPage } from './pages/ChatPage';
import {
  INITIAL_ONBOARDING_STEPS,
  OnboardingStep,
  AccessTask,
  fetchOnboardingPlan,
  fetchAccessTasks,
} from './lib/api';

export const App: React.FC = () => {
  const [steps, setSteps] = useState<OnboardingStep[]>(INITIAL_ONBOARDING_STEPS);
  const [tasks, setTasks] = useState<AccessTask[]>([]);

  useEffect(() => {
    fetchOnboardingPlan().then(setSteps);
    fetchAccessTasks().then(setTasks);
  }, []);

  const handleProposalConfirmed = (resourceId: string, issueKey: string) => {
    const newTask: AccessTask = {
      task_id: 'task-' + issueKey,
      proposal_id: 'prop-' + issueKey,
      resource_id: resourceId,
      title: 'Выдать стажёру доступ к пространству команды (' + resourceId + ')',
      assignee: 'Тимлид команды',
      tracker_issue_key: issueKey,
      status: 'approved',
      created_at: 'Сегодня',
    };

    setTasks((prev) => {
      const exists = prev.some((t) => t.resource_id === resourceId);
      return exists ? prev : [newTask, ...prev];
    });

    setSteps((prev) =>
      prev.map((s) =>
        s.resource_id === resourceId ? { ...s, completed: true, current: false } : s
      )
    );
  };

  return (
    <div style={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      {/* Верхняя шапка с логотипом AI BUDDY и 4 кружочками без рамки кнопки */}
      <header className="app-header">
        <div className="aibuddy-logo-pill">
          <span className="aibuddy-logo-dots" aria-hidden="true">
            <span className="aibuddy-logo-dot" style={{ background: '#04E061' }} />
            <span className="aibuddy-logo-dot" style={{ background: '#00AAFF' }} />
            <span className="aibuddy-logo-dot" style={{ background: '#965EEB' }} />
            <span className="aibuddy-logo-dot" style={{ background: '#FF4053' }} />
          </span>
          <span>AI BUDDY</span>
        </div>
      </header>

      {/* Основной экран чата */}
      <ChatPage
        steps={steps}
        tasks={tasks}
        onProposalConfirmed={handleProposalConfirmed}
      />
    </div>
  );
};

export default App;
