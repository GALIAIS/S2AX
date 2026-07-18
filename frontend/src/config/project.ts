const configuredRepository = String(import.meta.env.VITE_PROJECT_REPOSITORY || '').trim()

export const PROJECT_REPOSITORY = configuredRepository || 'GALIAIS/S2AX'
export const PROJECT_URL = `https://github.com/${PROJECT_REPOSITORY}`
export const PROJECT_RELEASES_URL = `${PROJECT_URL}/releases`
