type StartupPath = "/api/v1/account" | "/api/v1/me/preferences"

declare global {
  interface Window {
    __savorumStartup?: Map<StartupPath, Promise<Response | undefined>>
  }
}

/** Unscoped reads overlap module downloads and are consumed once in this document. */
export const startupBootScript = `(()=>{const requests=new Map();for(const path of ["/api/v1/account","/api/v1/me/preferences"])requests.set(path,fetch(path,{credentials:"same-origin"}).catch(()=>undefined));window.__savorumStartup=requests})()`

export function takeStartupResponse(path: StartupPath): Promise<Response | undefined> | undefined {
  if (typeof window === "undefined") return
  const requests = window.__savorumStartup
  const response = requests?.get(path)
  requests?.delete(path)
  if (requests?.size === 0) delete window.__savorumStartup
  return response
}
