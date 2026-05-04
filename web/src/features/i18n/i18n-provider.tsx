'use client'

import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'

import { messages, type Language } from '@/features/i18n/messages'

interface I18nContextValue {
  currentLang: Language
  setLang: (lang: Language) => void
  t: (key: keyof (typeof messages)['en']) => string
}

const I18nContext = createContext<I18nContextValue | null>(null)

function readLanguage(): Language {
  if (typeof window === 'undefined') return 'en'
  return window.localStorage.getItem('acp-lang') === 'zh' ? 'zh' : 'en'
}

export function I18nProvider({ children }: { children: ReactNode }) {
  const [currentLang, setCurrentLang] = useState<Language>('en')

  useEffect(() => {
    setCurrentLang(readLanguage())
  }, [])

  const setLang = (lang: Language) => {
    setCurrentLang(lang)
    window.localStorage.setItem('acp-lang', lang)
  }

  const t = (key: keyof (typeof messages)['en']) => messages[currentLang][key] || key

  return (
    <I18nContext.Provider value={{ currentLang, setLang, t }}>
      {children}
    </I18nContext.Provider>
  )
}

export function useI18n() {
  const context = useContext(I18nContext)
  if (!context) {
    throw new Error('useI18n must be used within I18nProvider')
  }

  return context
}
