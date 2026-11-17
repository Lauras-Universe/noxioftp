export namespace connections {
	
	export class Profile {
	    id: string;
	    name: string;
	    protocol: string;
	    host: string;
	    port: number;
	    user: string;
	    authType: string;
	    keyPath: string;
	    remoteDir: string;
	    jumpId: string;
	    hasSecret: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Profile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.protocol = source["protocol"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.user = source["user"];
	        this.authType = source["authType"];
	        this.keyPath = source["keyPath"];
	        this.remoteDir = source["remoteDir"];
	        this.jumpId = source["jumpId"];
	        this.hasSecret = source["hasSecret"];
	    }
	}

}

export namespace fsinfo {
	
	export class Crumb {
	    name: string;
	    path: string;
	
	    static createFrom(source: any = {}) {
	        return new Crumb(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	    }
	}
	export class Entry {
	    name: string;
	    path: string;
	    size: number;
	    isDir: boolean;
	    isLink: boolean;
	    mode: string;
	    perm: number;
	    modTime: number;
	
	    static createFrom(source: any = {}) {
	        return new Entry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.size = source["size"];
	        this.isDir = source["isDir"];
	        this.isLink = source["isLink"];
	        this.mode = source["mode"];
	        this.perm = source["perm"];
	        this.modTime = source["modTime"];
	    }
	}
	export class Listing {
	    path: string;
	    parent: string;
	    crumbs: Crumb[];
	    entries: Entry[];
	
	    static createFrom(source: any = {}) {
	        return new Listing(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.parent = source["parent"];
	        this.crumbs = this.convertValues(source["crumbs"], Crumb);
	        this.entries = this.convertValues(source["entries"], Entry);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace main {
	
	export class ConnectResult {
	    home: string;
	
	    static createFrom(source: any = {}) {
	        return new ConnectResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.home = source["home"];
	    }
	}

}

export namespace remote {
	
	export class TextFile {
	    content: string;
	    modTime: number;
	    size: number;
	
	    static createFrom(source: any = {}) {
	        return new TextFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.content = source["content"];
	        this.modTime = source["modTime"];
	        this.size = source["size"];
	    }
	}

}

export namespace transfer {
	
	export class Job {
	    id: string;
	    direction: string;
	    name: string;
	    src: string;
	    dst: string;
	    isDir: boolean;
	    status: string;
	    done: number;
	    total: number;
	    error: string;
	    note: string;
	    policy: string;
	
	    static createFrom(source: any = {}) {
	        return new Job(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.direction = source["direction"];
	        this.name = source["name"];
	        this.src = source["src"];
	        this.dst = source["dst"];
	        this.isDir = source["isDir"];
	        this.status = source["status"];
	        this.done = source["done"];
	        this.total = source["total"];
	        this.error = source["error"];
	        this.note = source["note"];
	        this.policy = source["policy"];
	    }
	}
	export class SyncAction {
	    kind: string;
	    rel: string;
	    src: string;
	    dst: string;
	    size: number;
	    isDir: boolean;
	    reason: string;
	
	    static createFrom(source: any = {}) {
	        return new SyncAction(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.rel = source["rel"];
	        this.src = source["src"];
	        this.dst = source["dst"];
	        this.size = source["size"];
	        this.isDir = source["isDir"];
	        this.reason = source["reason"];
	    }
	}
	export class SyncOptions {
	    direction: string;
	    deleteExtra: boolean;
	    keepNewer: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SyncOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.direction = source["direction"];
	        this.deleteExtra = source["deleteExtra"];
	        this.keepNewer = source["keepNewer"];
	    }
	}
	export class SyncPlan {
	    id: string;
	    direction: string;
	    localDir: string;
	    remoteDir: string;
	    actions: SyncAction[];
	    copyCount: number;
	    deleteCount: number;
	    bytes: number;
	    note: string;
	
	    static createFrom(source: any = {}) {
	        return new SyncPlan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.direction = source["direction"];
	        this.localDir = source["localDir"];
	        this.remoteDir = source["remoteDir"];
	        this.actions = this.convertValues(source["actions"], SyncAction);
	        this.copyCount = source["copyCount"];
	        this.deleteCount = source["deleteCount"];
	        this.bytes = source["bytes"];
	        this.note = source["note"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace update {
	
	export class Info {
	    available: boolean;
	    current: string;
	    latest: string;
	    notes: string;
	    pageUrl: string;
	    method: string;
	    command: string;
	    reason: string;
	
	    static createFrom(source: any = {}) {
	        return new Info(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.available = source["available"];
	        this.current = source["current"];
	        this.latest = source["latest"];
	        this.notes = source["notes"];
	        this.pageUrl = source["pageUrl"];
	        this.method = source["method"];
	        this.command = source["command"];
	        this.reason = source["reason"];
	    }
	}

}

