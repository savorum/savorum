/** Serialize initial dish creation, including browsers on non-secure local origins. */
export async function withCookingLock<T>(key: string, operation: () => T): Promise<T> {
  if (typeof navigator !== "undefined" && navigator.locks)
    return navigator.locks.request(`savorum.cooking:${key}`, operation)
  if (typeof indexedDB === "undefined") return operation()
  const database = await new Promise<IDBDatabase>((resolve, reject) => {
    const request = indexedDB.open("savorum.cooking-locks", 1)
    request.onupgradeneeded = () => request.result.createObjectStore("locks")
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error)
  })
  try {
    return await new Promise<T>((resolve, reject) => {
      const transaction = database.transaction("locks", "readwrite")
      const request = transaction.objectStore("locks").get(key)
      let value: T
      request.onsuccess = () => {
        try {
          value = operation()
        } catch (error) {
          transaction.abort()
          reject(error)
        }
      }
      transaction.oncomplete = () => resolve(value)
      transaction.onerror = () => reject(transaction.error)
      transaction.onabort = () => reject(transaction.error)
    })
  } finally {
    database.close()
  }
}
