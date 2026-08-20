type EventCallback = (event: Event) => void

class MockEventTarget {
  private readonly listeners = new Map<string, EventCallback[]>()

  addEventListener(type: string, callback: EventCallback) {
    const callbacks = this.listeners.get(type) ?? []
    callbacks.push(callback)
    this.listeners.set(type, callbacks)
  }

  emit(type: string, event: Event = new Event(type)) {
    for (const callback of this.listeners.get(type) ?? []) {
      callback(event)
    }
  }
}

export class MockUploadXMLHttpRequest extends MockEventTarget {
  static instances: MockUploadXMLHttpRequest[] = []

  readonly upload = new MockEventTarget()
  readonly headers = new Map<string, string>()
  method = ""
  url = ""
  withCredentials = false
  status = 0
  responseText = ""
  body: XMLHttpRequestBodyInit | Document | null = null

  constructor() {
    super()
    MockUploadXMLHttpRequest.instances.push(this)
  }

  static reset() {
    MockUploadXMLHttpRequest.instances = []
  }

  open(method: string, url: string) {
    this.method = method
    this.url = url
  }

  setRequestHeader(name: string, value: string) {
    this.headers.set(name, value)
  }

  send(body: XMLHttpRequestBodyInit | Document | null) {
    this.body = body
  }

  reportProgress(loaded: number, total: number) {
    this.upload.emit("progress", {
      lengthComputable: true,
      loaded,
      total,
    } as ProgressEvent)
  }

  completeUpload() {
    this.upload.emit("load")
  }

  respond(status: number, payload: unknown) {
    this.status = status
    this.responseText = JSON.stringify(payload)
    this.emit("load")
  }

  fail() {
    this.emit("error")
  }
}
