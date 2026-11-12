import React from 'react';
import { AccessTask } from '../lib/api';
import { UserRole } from '../lib/cookies';

interface TasksPageProps {
  tasks: AccessTask[];
  role: UserRole;
  onApproveTask: (taskId: string) => void;
  onClose: () => void;
}

export const TasksPage: React.FC<TasksPageProps> = ({
  tasks,
  role,
  onApproveTask,
  onClose,
}) => {
  return (
    <div
      style={{
        position: 'fixed',
        inset: 0,
        background: 'rgba(20,20,20,0.45)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        zIndex: 50,
      }}
    >
      <div
        style={{
          width: '560px',
          maxWidth: '92vw',
          background: '#FFFFFF',
          border: '2px solid #141414',
          borderRadius: '18px',
          padding: '22px',
        }}
      >
        <div
          style={{
            display: 'flex',
            justifyContent: 'space-between',
            alignItems: 'center',
            paddingBottom: '12px',
            borderBottom: '2px solid #141414',
            marginBottom: '16px',
          }}
        >
          <div>
            <h3 style={{ fontSize: '18px', fontWeight: 800 }}>
              Сервис Task — Заявки на доступы
            </h3>
            <p style={{ fontSize: '12px', color: '#5C5C5C', marginTop: '2px' }}>
              GET /api/v1/tasks?kind=access_request • Текущая роль:{' '}
              <strong>{role === 'intern' ? 'Стажёр' : 'Тимлид (руководитель)'}</strong>
            </p>
          </div>
          <button
            onClick={onClose}
            style={{
              width: '32px',
              height: '32px',
              borderRadius: '50%',
              border: '2px solid #141414',
              background: '#FFFFFF',
              fontWeight: 800,
            }}
          >
            ✕
          </button>
        </div>

        {tasks.length === 0 ? (
          <div
            style={{
              padding: '24px',
              textAlign: 'center',
              border: '2px dashed #D6D6D6',
              borderRadius: '12px',
              color: '#5C5C5C',
              fontSize: '14px',
            }}
          >
            Пока нет созданных заявок. Нажми в чате «Как выполнить текущий шаг?» и подтверди заявку на доступ.
          </div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
            {tasks.map((t) => (
              <div
                key={t.task_id}
                style={{
                  border: '2px solid #141414',
                  borderRadius: '12px',
                  padding: '14px',
                  background: t.status === 'approved' ? '#F0FDF4' : '#EFF6FF',
                }}
              >
                <div
                  style={{
                    display: 'flex',
                    justifyContent: 'space-between',
                    alignItems: 'center',
                    marginBottom: '6px',
                  }}
                >
                  <span style={{ fontSize: '12px', fontWeight: 700, color: '#5C5C5C' }}>
                    Тикет #{t.tracker_issue_key} • Ресурс: {t.resource_id}
                  </span>
                  <span
                    style={{
                      fontSize: '11px',
                      fontWeight: 700,
                      padding: '3px 10px',
                      borderRadius: '999px',
                      border: '1.5px solid #141414',
                      background: t.status === 'approved' ? '#C9FFBF' : '#BADAFB',
                    }}
                  >
                    {t.status === 'approved'
                      ? '✓ Доступ выдан'
                      : 'Ожидает тимлида (pending_approval)'}
                  </span>
                </div>
                <div style={{ fontSize: '14px', fontWeight: 700, marginBottom: '4px' }}>
                  {t.title}
                </div>
                <div style={{ fontSize: '12px', color: '#5C5C5C' }}>
                  Исполнитель: {t.assignee}
                </div>

                {t.status !== 'approved' && (
                  <button
                    onClick={() => onApproveTask(t.task_id)}
                    style={{
                      marginTop: '10px',
                      padding: '8px 14px',
                      borderRadius: '8px',
                      border: '2px solid #141414',
                      background: '#04E061',
                      fontWeight: 700,
                      fontSize: '12px',
                    }}
                  >
                    ✓ Одобрить доступ (как Тимлид) и закрыть шаг в Roadmap
                  </button>
                )}
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
};
