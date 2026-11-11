import React, { useState, useEffect } from 'react';
import { ChatPage } from './pages/ChatPage';
import { TasksPage } from './pages/TasksPage';
import { ProfilePage } from './pages/ProfilePage';
import {
  INITIAL_ONBOARDING_STEPS,
  OnboardingStep,
  AccessTask,
  fetchOnboardingPlan,
  fetchAccessTasks,
} from './lib/api';
import { getSavedRole, setSavedRole, UserRole } from './lib/cookies';

export const App: React.FC = () => {
  const [steps, setSteps] = useState<OnboardingStep[]>(INITIAL_ONBOARDING_STEPS);
  const [tasks, setTasks] = useState<AccessTask[]>([]);
  const [role, setRole] = useState<UserRole>(getSavedRole());
  const [showTasksModal, setShowTasksModal] = useState(false);
  const [showProfileModal, setShowProfileModal] = useState(false);

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

  const handleApproveTask = (taskId: string) => {
    let approvedResource = '';
    setTasks((prev) =>
      prev.map((t) => {
        if (t.task_id === taskId) {
          approvedResource = t.resource_id;
          return { ...t, status: 'approved' };
        }
        return t;
      })
    );
    if (approvedResource) {
      setSteps((prev) =>
        prev.map((s) =>
          s.resource_id === approvedResource ? { ...s, completed: true } : s
        )
      );
    }
  };

  const handleRoleChange = (newRole: UserRole) => {
    setRole(newRole);
    setSavedRole(newRole);
    setShowProfileModal(false);
  };

  return (
    <div style={{ height: '100vh', display: 'flex', flexDirection: 'column' }}>
      {/* Верхняя шапка ровно как на макете Miro */}
      <header
        style={{
          height: '68px',
          minHeight: '68px',
          borderBottom: '2px solid #141414',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          position: 'relative',
          padding: '0 24px',
          background: '#FFFFFF',
        }}
      >
        <div className="aibuddy-logo-pill">AIBUDDY</div>

        {/* Ненавязчивые кнопки справа для показа жюри Task-трекера и Роли */}
        <div
          style={{
            position: 'absolute',
            right: '24px',
            display: 'flex',
            alignItems: 'center',
            gap: '10px',
          }}
        >
          <button
            onClick={() => setShowTasksModal(true)}
            style={{
              padding: '6px 12px',
              borderRadius: '999px',
              border: '1.5px solid #141414',
              background: '#FFFFFF',
              fontSize: '12px',
              fontWeight: 700,
            }}
          >
            Заявки Task ({tasks.length})
          </button>

          <button
            onClick={() => setShowProfileModal(true)}
            style={{
              padding: '6px 12px',
              borderRadius: '999px',
              border: '1.5px solid #141414',
              background: role === 'intern' ? '#C9FFBF' : '#DFCDF3',
              fontSize: '12px',
              fontWeight: 700,
            }}
          >
            Роль: {role === 'intern' ? 'Стажёр' : 'Тимлид'}
          </button>
        </div>
      </header>

      {/* Основной экран чата */}
      <ChatPage
        steps={steps}
        tasks={tasks}
        onProposalConfirmed={handleProposalConfirmed}
      />

      {showTasksModal && (
        <TasksPage
          tasks={tasks}
          role={role}
          onApproveTask={handleApproveTask}
          onClose={() => setShowTasksModal(false)}
        />
      )}

      {showProfileModal && (
        <ProfilePage
          role={role}
          onChangeRole={handleRoleChange}
          onClose={() => setShowProfileModal(false)}
        />
      )}
    </div>
  );
};

export default App;
