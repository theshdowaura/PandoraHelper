import{j as t}from"./index-782b2cb8.js";import{u as o,C as s}from"./useChart-de996a9a.js";const r=[{name:"Net Profit",data:[44,55,57,56,61,58,63,60,66]}];function a(){const a=o({plotOptions:{bar:{columnWidth:"16%"}},stroke:{show:!1},xaxis:{categories:["Feb","Mar","Apr","May","Jun","Jul","Aug","Sep","Oct"]},tooltip:{y:{formatter:t=>`$ ${t} thousands`}}});return t.jsx(s,{type:"bar",series:r,options:a,height:320})}export{a as default};