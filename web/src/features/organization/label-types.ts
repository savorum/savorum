export interface Label {
  id: string
  name: string
  color?: string
  revision: number
}
export interface Group {
  key: string
  title: string
  singular: string
  placeholder: string
  colored: boolean
  labels: Label[]
  create: (name: string, color?: string) => Promise<unknown>
  rename: (label: Label, name: string, color?: string) => Promise<unknown>
  merge: (label: Label, targetID: string) => Promise<unknown>
  remove: (label: Label) => Promise<unknown>
  /** How many recipes carry this label. */
  usage: (label: Label) => Promise<number>
}
