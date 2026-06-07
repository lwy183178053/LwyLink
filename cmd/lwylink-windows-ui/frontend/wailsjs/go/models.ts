export namespace account {
	
	export class PublicUser {
	    id: number;
	    username: string;
	    email: string;
	    displayName: string;
	    role: string;
	    membership: string;
	    disabled: boolean;
	    relayTrafficUsedBytes: number;
	    relayTrafficExtraBytes: number;
	    relayTrafficQuotaBytes: number;
	    relayTrafficRemainingBytes: number;
	    relayTrafficPeriod: string;
	    // Go type: time
	    createdAt: any;
	
	    static createFrom(source: any = {}) {
	        return new PublicUser(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.username = source["username"];
	        this.email = source["email"];
	        this.displayName = source["displayName"];
	        this.role = source["role"];
	        this.membership = source["membership"];
	        this.disabled = source["disabled"];
	        this.relayTrafficUsedBytes = source["relayTrafficUsedBytes"];
	        this.relayTrafficExtraBytes = source["relayTrafficExtraBytes"];
	        this.relayTrafficQuotaBytes = source["relayTrafficQuotaBytes"];
	        this.relayTrafficRemainingBytes = source["relayTrafficRemainingBytes"];
	        this.relayTrafficPeriod = source["relayTrafficPeriod"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
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
	
	export class UpdateState {
	    status: string;
	    message: string;
	    currentVersion: string;
	    latestVersion: string;
	    platform: string;
	    notes: string;
	    mandatory: boolean;
	    installArgs: string;
	    fileName: string;
	    sizeBytes: number;
	    sha256: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.message = source["message"];
	        this.currentVersion = source["currentVersion"];
	        this.latestVersion = source["latestVersion"];
	        this.platform = source["platform"];
	        this.notes = source["notes"];
	        this.mandatory = source["mandatory"];
	        this.installArgs = source["installArgs"];
	        this.fileName = source["fileName"];
	        this.sizeBytes = source["sizeBytes"];
	        this.sha256 = source["sha256"];
	    }
	}
	export class ConnectionState {
	    status: string;
	    message: string;
	    roomID: number;
	    ip: string;
	    directPeerIPs: string[];
	
	    static createFrom(source: any = {}) {
	        return new ConnectionState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.message = source["message"];
	        this.roomID = source["roomID"];
	        this.ip = source["ip"];
	        this.directPeerIPs = source["directPeerIPs"];
	    }
	}
	export class AppState {
	    apiBaseURL: string;
	    interfaceName: string;
	    p2pEnabled: boolean;
	    user?: account.PublicUser;
	    rooms: room.Room[];
	    currentRoom?: room.Room;
	    members: room.MemberView[];
	    chatMessages: room.ChatMessage[];
	    connection: ConnectionState;
	    update: UpdateState;
	    announcement: string;
	
	    static createFrom(source: any = {}) {
	        return new AppState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.apiBaseURL = source["apiBaseURL"];
	        this.interfaceName = source["interfaceName"];
	        this.p2pEnabled = source["p2pEnabled"];
	        this.user = this.convertValues(source["user"], account.PublicUser);
	        this.rooms = this.convertValues(source["rooms"], room.Room);
	        this.currentRoom = this.convertValues(source["currentRoom"], room.Room);
	        this.members = this.convertValues(source["members"], room.MemberView);
	        this.chatMessages = this.convertValues(source["chatMessages"], room.ChatMessage);
	        this.connection = this.convertValues(source["connection"], ConnectionState);
	        this.update = this.convertValues(source["update"], UpdateState);
	        this.announcement = source["announcement"];
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
	
	export class CreateRoomInput {
	    name: string;
	    maxMembers: number;
	
	    static createFrom(source: any = {}) {
	        return new CreateRoomInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.maxMembers = source["maxMembers"];
	    }
	}
	export class JoinRoomInput {
	    roomPublicKey: string;
	
	    static createFrom(source: any = {}) {
	        return new JoinRoomInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.roomPublicKey = source["roomPublicKey"];
	    }
	}
	export class LoginInput {
	    email: string;
	    username: string;
	    password: string;
	
	    static createFrom(source: any = {}) {
	        return new LoginInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.email = source["email"];
	        this.username = source["username"];
	        this.password = source["password"];
	    }
	}
	export class MemberActionInput {
	    userID: number;
	
	    static createFrom(source: any = {}) {
	        return new MemberActionInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.userID = source["userID"];
	    }
	}
	export class RegisterInput {
	    email: string;
	    username: string;
	    password: string;
	    displayName: string;
	
	    static createFrom(source: any = {}) {
	        return new RegisterInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.email = source["email"];
	        this.username = source["username"];
	        this.password = source["password"];
	        this.displayName = source["displayName"];
	    }
	}
	export class SendChatInput {
	    roomID: number;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new SendChatInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.roomID = source["roomID"];
	        this.message = source["message"];
	    }
	}
	export class SettingsInput {
	    apiBaseURL: string;
	    interfaceName: string;
	    p2pEnabled?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SettingsInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.apiBaseURL = source["apiBaseURL"];
	        this.interfaceName = source["interfaceName"];
	        this.p2pEnabled = source["p2pEnabled"];
	    }
	}

}

export namespace room {
	
	export class ChatMessage {
	    id: string;
	    roomID: number;
	    userID: number;
	    username: string;
	    displayName: string;
	    message: string;
	    // Go type: time
	    createdAt: any;
	
	    static createFrom(source: any = {}) {
	        return new ChatMessage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.roomID = source["roomID"];
	        this.userID = source["userID"];
	        this.username = source["username"];
	        this.displayName = source["displayName"];
	        this.message = source["message"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
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
	export class MemberView {
	    roomID: number;
	    userID: number;
	    username: string;
	    displayName: string;
	    role: string;
	    online: boolean;
	    virtualIP?: string;
	    // Go type: time
	    createdAt: any;
	
	    static createFrom(source: any = {}) {
	        return new MemberView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.roomID = source["roomID"];
	        this.userID = source["userID"];
	        this.username = source["username"];
	        this.displayName = source["displayName"];
	        this.role = source["role"];
	        this.online = source["online"];
	        this.virtualIP = source["virtualIP"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
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
	export class Room {
	    id: number;
	    name: string;
	    ownerUserID: number;
	    nodeID: number;
	    roomPublicKey: string;
	    networkCIDR: string;
	    maxMembers: number;
	    memberCount: number;
	    status: string;
	    nodeName?: string;
	    nodeOverlayAddr?: string;
	    nodeUDPAddr?: string;
	    nodePublicKey?: string;
	    // Go type: time
	    createdAt: any;
	    // Go type: time
	    updatedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new Room(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.ownerUserID = source["ownerUserID"];
	        this.nodeID = source["nodeID"];
	        this.roomPublicKey = source["roomPublicKey"];
	        this.networkCIDR = source["networkCIDR"];
	        this.maxMembers = source["maxMembers"];
	        this.memberCount = source["memberCount"];
	        this.status = source["status"];
	        this.nodeName = source["nodeName"];
	        this.nodeOverlayAddr = source["nodeOverlayAddr"];
	        this.nodeUDPAddr = source["nodeUDPAddr"];
	        this.nodePublicKey = source["nodePublicKey"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.updatedAt = this.convertValues(source["updatedAt"], null);
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

