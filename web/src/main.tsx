import '@mantine/core/styles.css'
import '@mantine/notifications/styles.css'

import { MantineProvider, Title } from '@mantine/core'
import { Notifications } from '@mantine/notifications'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { theme } from './theme'

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false, refetchOnWindowFocus: false } },
})

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <MantineProvider theme={theme} defaultColorScheme="dark">
      <Notifications />
      <QueryClientProvider client={queryClient}>
        <Title order={2} p="md">
          softwarr
        </Title>
      </QueryClientProvider>
    </MantineProvider>
  </StrictMode>,
)
