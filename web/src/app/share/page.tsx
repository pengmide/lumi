import { Suspense } from 'react'

import { SharedConversationPage } from '@/features/share/shared-conversation-page'

export default function SharePage() {
  return (
    <Suspense fallback={null}>
      <SharedConversationPage />
    </Suspense>
  )
}
