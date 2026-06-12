export namespace config {
	
	export class Config {
	    instancesDir: string;
	    launcher: string;
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.instancesDir = source["instancesDir"];
	        this.launcher = source["launcher"];
	    }
	}

}

export namespace instance {
	
	export class Marker {
	    serverId: string;
	    token: string;
	    baseUrl: string;
	    lastSyncAt: string;
	    lastCheckAt: string;
	    expiresAt?: string;
	
	    static createFrom(source: any = {}) {
	        return new Marker(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serverId = source["serverId"];
	        this.token = source["token"];
	        this.baseUrl = source["baseUrl"];
	        this.lastSyncAt = source["lastSyncAt"];
	        this.lastCheckAt = source["lastCheckAt"];
	        this.expiresAt = source["expiresAt"];
	    }
	}
	export class ScannedInstance {
	    Dir: string;
	    Name: string;
	    Marker?: Marker;
	
	    static createFrom(source: any = {}) {
	        return new ScannedInstance(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Dir = source["Dir"];
	        this.Name = source["Name"];
	        this.Marker = this.convertValues(source["Marker"], Marker);
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

export namespace sync {
	
	export class InfoResponse {
	    token: string;
	    serverName: string;
	    expiresAt: string;
	    createdAt: string;
	    formats: string[];
	
	    static createFrom(source: any = {}) {
	        return new InfoResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.token = source["token"];
	        this.serverName = source["serverName"];
	        this.expiresAt = source["expiresAt"];
	        this.createdAt = source["createdAt"];
	        this.formats = source["formats"];
	    }
	}

}

