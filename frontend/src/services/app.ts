import { Call } from '@wailsio/runtime'

// Route types
export interface Route {
  id: number
  name: string
  model: string
  api_url: string
  api_key: string
  group: string
  format: string
  enabled: boolean
  created: string
  updated: string
}

// Stats types
export interface Stats {
  route_count: number
  model_count: number
  total_requests: number
  total_tokens: number
  today_requests: number
  today_tokens: number
  success_rate: number
}

// Config types
export interface Config {
  localApiKey: string
  openaiEndpoint: string
  redirectEnabled: boolean
  redirectKeyword: string
  redirectTargetModel: string
  redirectTargetName: string
  minimizeToTray: boolean
  autoStart: boolean
  enableFileLog: boolean
}

// App settings types
export interface AppSettings {
  minimizeToTray: boolean
  autoStart: boolean
  autoStartEnabled: boolean
}

// Daily stats types
export interface DailyStats {
  date: string
  requests: number
  request_tokens: number
  response_tokens: number
  total_tokens: number
}

// Hourly stats types
export interface HourlyStats {
  hour: number
  requests: number
  total_tokens: number
}

// Model ranking types
export interface ModelRanking {
  rank: number
  model: string
  requests: number
  request_tokens: number
  response_tokens: number
  total_tokens: number
  success_rate: number
}


// Service name prefix for Wails v3: module/package.struct
const SERVICE = 'openai-router-go/services.AppService'

// Helper function to call service methods
const callService = <T>(method: string, ...args: unknown[]): Promise<T> => {
  return Call.ByName(`${SERVICE}.${method}`, ...args)
}

// Route management
export const getRoutes = async (): Promise<Route[]> => {
  return callService<Route[]>('GetRoutes')
}

export const addRoute = async (
  name: string,
  model: string,
  apiUrl: string,
  apiKey: string,
  group: string,
  format: string
): Promise<void> => {
  return callService<void>('AddRoute', name, model, apiUrl, apiKey, group, format)
}

export const addRoutes = async (
  baseName: string,
  models: string[],
  apiUrl: string,
  apiKey: string,
  group: string,
  format: string
): Promise<void> => {
  return callService<void>('AddRoutes', baseName, models, apiUrl, apiKey, group, format)
}

export const updateRoute = async (
  id: number,
  name: string,
  model: string,
  apiUrl: string,
  apiKey: string,
  group: string,
  format: string
): Promise<void> => {
  return callService<void>('UpdateRoute', id, name, model, apiUrl, apiKey, group, format)
}

// Update route by composite key (name + model)
export const updateRouteByKey = async (
  oldName: string,
  oldModel: string,
  name: string,
  model: string,
  apiUrl: string,
  apiKey: string,
  group: string,
  format: string
): Promise<void> => {
  return callService<void>('UpdateRouteByKey', oldName, oldModel, name, model, apiUrl, apiKey, group, format)
}

export const deleteRoute = async (id: number): Promise<void> => {
  return callService<void>('DeleteRoute', id)
}

// Delete route by composite key (name + model)
export const deleteRouteByKey = async (name: string, model: string): Promise<void> => {
  return callService<void>('DeleteRouteByKey', name, model)
}

export const clearAllRoutes = async (): Promise<void> => {
  return callService<void>('ClearAllRoutes')
}

export const hasMultiModelRoutes = async (): Promise<boolean> => {
  return callService<boolean>('HasMultiModelRoutes')
}

// Statistics
export const getStats = async (): Promise<Stats> => {
  return callService<Stats>('GetStats')
}

export const getDailyStats = async (days: number): Promise<DailyStats[]> => {
  return callService<DailyStats[]>('GetDailyStats', days)
}

export const getHourlyStats = async (): Promise<HourlyStats[]> => {
  return callService<HourlyStats[]>('GetHourlyStats')
}

export const getModelRanking = async (limit: number): Promise<ModelRanking[]> => {
  return callService<ModelRanking[]>('GetModelRanking', limit)
}

export const clearStats = async (): Promise<void> => {
  return callService<void>('ClearStats')
}

export const compressRequestLogs = async (): Promise<number> => {
  return callService<number>('CompressRequestLogs')
}

// Configuration
export const getConfig = async (): Promise<Config> => {
  return callService<Config>('GetConfig')
}

export const updateConfig = async (
  redirectEnabled: boolean,
  redirectKeyword: string,
  redirectTargetModel: string
): Promise<void> => {
  return callService<void>('UpdateConfig', redirectEnabled, redirectKeyword, redirectTargetModel)
}

export const updateLocalApiKey = async (newApiKey: string): Promise<void> => {
  return callService<void>('UpdateLocalApiKey', newApiKey)
}

// App settings
export const getAppSettings = async (): Promise<AppSettings> => {
  return callService<AppSettings>('GetAppSettings')
}

export const setMinimizeToTray = async (enabled: boolean): Promise<void> => {
  return callService<void>('SetMinimizeToTray', enabled)
}

export const setAutoStart = async (enabled: boolean): Promise<void> => {
  return callService<void>('SetAutoStart', enabled)
}

export const setEnableFileLog = async (enabled: boolean): Promise<void> => {
  return callService<void>('SetEnableFileLog', enabled)
}

// Remote models
export const fetchRemoteModels = async (apiUrl: string, apiKey: string): Promise<string[]> => {
  return callService<string[]>('FetchRemoteModels', apiUrl, apiKey)
}

// Import
export const importRouteFromFormat = async (
  name: string,
  model: string,
  apiUrl: string,
  apiKey: string,
  group: string,
  targetFormat: string
): Promise<string> => {
  return callService<string>('ImportRouteFromFormat', name, model, apiUrl, apiKey, group, targetFormat)
}
