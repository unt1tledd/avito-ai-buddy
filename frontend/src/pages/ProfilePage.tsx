import React from 'react';
import { UserRole } from '../lib/cookies';

interface ProfilePageProps {
  role: UserRole;
  onChangeRole: (role: UserRole) => void;
  onClose: () => void;
}

export const ProfilePage: React.FC<ProfilePageProps> = ({
  role,
  onChangeRole,
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
          width: '440px',
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
          <h3 style={{ fontSize: '18px', fontWeight: 800 }}>Профиль и роль (RBAC)</h3>
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

        <p style={{ fontSize: '13px', color: '#5C5C5C', marginBottom: '14px', lineHeight: 1.4 }}>
          По ограничению №05 кейса Авито AI-buddy учитывает права пользователя. Переключи роль, чтобы показать жюри работу со стороны стажёра и тимлида:
        </p>

        <div style={{ display: 'flex', flexDirection: 'column', gap: '10px' }}>
          <button
            onClick={() => onChangeRole('intern')}
            style={{
              padding: '12px 14px',
              borderRadius: '12px',
              border: '2px solid #141414',
              background: role === 'intern' ? '#C9FFBF' : '#FFFFFF',
              textAlign: 'left',
            }}
          >
            <div style={{ fontWeight: 800, fontSize: '14px' }}>
              Стажёр Backend-команды (Основной пользователь)
            </div>
            <div style={{ fontSize: '12px', color: '#5C5C5C', marginTop: '2px' }}>
              Кластер: Avito Goods • Проходит чек-лист 1-й недели и запрашивает доступы
            </div>
          </button>

          <button
            onClick={() => onChangeRole('teamlead')}
            style={{
              padding: '12px 14px',
              borderRadius: '12px',
              border: '2px solid #141414',
              background: role === 'teamlead' ? '#DFCDF3' : '#FFFFFF',
              textAlign: 'left',
            }}
          >
            <div style={{ fontWeight: 800, fontSize: '14px' }}>
              Тимлид / Наставник (Buddy)
            </div>
            <div style={{ fontSize: '12px', color: '#5C5C5C', marginTop: '2px' }}>
              Подтверждает заявки в Task-трекере и контролирует закрытие шагов адаптации
            </div>
          </button>
        </div>
      </div>
    </div>
  );
};
