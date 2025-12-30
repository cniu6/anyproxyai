/**
 * Wails v3 Compatibility Shim
 * 
 * This file provides backward compatibility for Wails v2 style calls (window.go.main.App.*)
 * by mapping them to Wails v3 Call API.
 */

import { Call } from '@wailsio/runtime'

// Wails v3 service name format: module/package.struct
// Format: openai-router-go/services.AppService
const SERVICE = 'openai-router-go/services.AppService'

// Helper function to call service methods
// Wails v3 Call.ByName expects method name and arguments as separate parameters
const callService = async (method, ...args) => {
  const fullMethod = `${SERVICE}.${method}`
  console.log(`Calling: ${fullMethod}`, args)
  try {
    // Try with spread args first
    const result = await Call.ByName(fullMethod, ...args)
    console.log(`Result from ${fullMethod}:`, result)
    return result
  } catch (error) {
    console.error(`Error calling ${fullMethod}:`, error)
    throw error
  }
}

// Create the window.go.main.App object for backward compatibility
const createWailsShim = () => {
  const App = {
    // Route management
    GetRoutes: () => callService('GetRoutes'),
    AddRoute: (name, model, apiUrl, apiKey, group, format) => 
      callService('AddRoute', name, model, apiUrl, apiKey, group, format),
    UpdateRoute: (id, name, model, apiUrl, apiKey, group, format) => 
      callService('UpdateRoute', id, name, model, apiUrl, apiKey, group, format),
    DeleteRoute: (id) => callService('DeleteRoute', id),
    
    // Statistics
    GetStats: () => callService('GetStats'),
    GetDailyStats: (days) => callService('GetDailyStats', days),
    GetHourlyStats: () => callService('GetHourlyStats'),
    GetModelRanking: (limit) => callService('GetModelRanking', limit),
    ClearStats: () => callService('ClearStats'),
    CompressRequestLogs: () => callService('CompressRequestLogs'),
    
    // Configuration
    GetConfig: () => callService('GetConfig'),
    UpdateConfig: (redirectEnabled, redirectKeyword, redirectTargetModel, redirectTargetRouteId) => 
      callService('UpdateConfig', redirectEnabled, redirectKeyword, redirectTargetModel, redirectTargetRouteId),
    UpdatePort: (port) => callService('UpdatePort', port),
    UpdateLocalApiKey: (newApiKey) => callService('UpdateLocalApiKey', newApiKey),
    RestartApp: () => callService('RestartApp'),
    
    // App settings
    GetAppSettings: () => callService('GetAppSettings'),
    SetMinimizeToTray: (enabled) => callService('SetMinimizeToTray', enabled),
    SetAutoStart: (enabled) => callService('SetAutoStart', enabled),
    SetEnableFileLog: (enabled) => callService('SetEnableFileLog', enabled),
    
    // Remote models
    FetchRemoteModels: (apiUrl, apiKey) => callService('FetchRemoteModels', apiUrl, apiKey),
    
    // Import
    ImportRouteFromFormat: (name, model, apiUrl, apiKey, group, targetFormat) => 
      callService('ImportRouteFromFormat', name, model, apiUrl, apiKey, group, targetFormat),
  }

  // Create the window.go.main.App structure
  window.go = {
    main: {
      App: App
    }
  }
  
  console.log('Wails v3 shim initialized')
}

// Initialize the shim
createWailsShim()

export default createWailsShim
